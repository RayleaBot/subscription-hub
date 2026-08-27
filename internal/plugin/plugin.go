package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/assets"
)

const (
	schedulerTaskID              = "subscription-hub-check"
	schedulerCron                = "*/1 * * * *"
	defaultDeliveryMaxAgeMinutes = 30
	minimumDeliveryMaxAgeMinutes = 1
	maximumDeliveryMaxAgeMinutes = 24 * 60
	interactiveReplyTimeout      = 50 * time.Second
)

var schedulerRegistered atomic.Bool

type settings struct {
	Enabled               bool           `json:"enabled"`
	DeliveryMaxAgeMinutes int            `json:"delivery_max_age_minutes"`
	Subscriptions         []subscription `json:"subscriptions"`
}

type subscription struct {
	ID          string       `json:"id"`
	Platform    string       `json:"platform"`
	UID         string       `json:"uid"`
	Name        string       `json:"name"`
	AvatarURL   string       `json:"avatar_url,omitempty"`
	TargetType  string       `json:"target_type"`
	TargetID    string       `json:"target_id"`
	TargetName  string       `json:"target_name,omitempty"`
	Services    []string     `json:"services"`
	Subscribers []subscriber `json:"subscribers"`
	Enabled     bool         `json:"enabled"`
}

type subscriber struct {
	ID            string `json:"id"`
	Nickname      string `json:"nickname"`
	GroupNickname string `json:"group_nickname,omitempty"`
	Title         string `json:"title,omitempty"`
	BaseRole      string `json:"base_role,omitempty"`
	Role          string `json:"role,omitempty"`
	RoleLabel     string `json:"role_label,omitempty"`
	AvatarURL     string `json:"avatar_url,omitempty"`
}

func Run(ctx context.Context) error {
	return rayleabot.Run(ctx, rayleabot.Options{
		PluginID: "raylea.subscription-hub",
		Subscriptions: []string{
			"plugin.started", "config.changed", "scheduler.trigger", "management.action",
		},
		MaxConcurrentHandlers: 1,
	}, rayleabot.HandlerFunc(handleEvent))
}

func handleEvent(ctx context.Context, event *rayleabot.EventContext) error {
	switch event.Event.EventType {
	case "plugin.started":
		registered := ensureScheduler(ctx, event)
		return event.Result(map[string]any{"handled": true, "scheduler_registered": registered})
	case "config.changed":
		_, _ = loadSettings(ctx, event)
		return event.Result(map[string]any{"handled": true, "reloaded": true})
	case "scheduler.trigger":
		action := stringScalar(event.Event.Payload["action"])
		if action == "" {
			action = stringScalar(nestedValue(event.Event.Payload, "payload", "action"))
		}
		if action != "" && action != "check_subscriptions" {
			return event.Result(map[string]any{"handled": false})
		}
		// cron 固定整分钟触发，无 jitter 时所有订阅检查在同一秒发起
		// 上游请求；随机延迟摊平尖峰。
		if delay := schedulerJitterDelay(); delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		checkEventCtx, cancel := context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
		current, err := loadSettings(checkEventCtx, event)
		if err != nil {
			return err
		}
		result := checkSubscriptions(checkEventCtx, event, current)
		logSubscriptionCheck(checkEventCtx, event.Actions(), result)
		return event.Result(result)
	case "management.action":
		return handleManagementAction(ctx, event)
	}
	return handleCommand(ctx, event)
}

func handleCommand(ctx context.Context, event *rayleabot.EventContext) error {
	command := strings.TrimSpace(event.Event.Command())
	if command == "" {
		return event.Result(map[string]any{"handled": false})
	}
	platform, operation := commandOperation(command)
	if operation == "" {
		return event.Result(map[string]any{"handled": false})
	}

	// 搜索、订阅检查、订阅变更和预览会串联配置读写、第三方请求、头像内联与图片渲染。
	// 为默认 60s 插件事件期限保留终态回复余量，并让各阶段共享同一个总预算。
	commandCtx := ctx
	cancel := func() {}
	if interactiveCommandOperation(operation) {
		commandCtx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
	}
	defer cancel()

	if operation == "search" {
		query := strings.Join(event.Event.Args(), " ")
		if platform == "weibo" {
			return replyWeiboUserSearch(commandCtx, event, query)
		}
		return replyBilibiliUserSearch(commandCtx, event, query)
	}
	if operation == "preview" {
		return previewSubscriptionCard(commandCtx, event, strings.Join(event.Event.Args(), " "))
	}

	current, err := loadSettings(commandCtx, event)
	if err != nil {
		return err
	}
	switch operation {
	case "status":
		return event.SendText(formatStatus(current))
	case "add":
		outcome := addSubscription(commandCtx, &current, event, platform)
		if outcome.Changed {
			if err := saveSettings(commandCtx, event, current); err != nil {
				return err
			}
		}
		return replySubscriptionOutcome(commandCtx, event, platform, outcome)
	case "remove":
		outcome := removeSubscription(&current, event, platform)
		if outcome.Changed {
			cleanupRemovedSubscriptionKV(commandCtx, event.Actions(), &current, outcome.Item, outcome.Services)
			if err := saveSettings(commandCtx, event, current); err != nil {
				return err
			}
		}
		return replySubscriptionOutcome(commandCtx, event, platform, outcome)
	case "list", "list_all":
		return event.SendText(formatSubscriptions(current, event, platform, operation == "list_all"))
	case "check":
		result := checkSubscriptions(commandCtx, event, current)
		return event.SendText(subscriptionCheckSummary(result))
	}
	return event.Result(map[string]any{"handled": false})
}

func interactiveCommandOperation(operation string) bool {
	switch operation {
	case "add", "remove", "search", "preview", "check":
		return true
	default:
		return false
	}
}

// replySubscriptionOutcome 回复订阅变更结果：bilibili/微博平台成功时发送资料卡片，
// 其余情况（其他平台、未变更、渲染失败）回退为文字。
func replySubscriptionOutcome(ctx context.Context, event *rayleabot.EventContext, platform string, outcome subscriptionOutcome) error {
	if outcome.Action == "candidates" {
		return replySubscriptionCandidates(ctx, event, platform, outcome)
	}
	if !outcome.Changed || outcome.Item == nil {
		return event.SendText(outcome.Message)
	}
	item := *outcome.Item
	switch platform {
	case "bilibili":
		user := bilibiliUser{UID: item.UID, Name: item.Name, AvatarURL: item.AvatarURL}
		if outcome.User != nil {
			user = *outcome.User
		}
		// 头像内联为 dataURL；内联失败时回退模板默认头像，不再把远程 URL 交给渲染。
		user.AvatarURL = inlineBilibiliCardAvatar(ctx, event.Actions(), firstText(user.AvatarURL, item.AvatarURL))
		item.AvatarURL = ""
		data := buildBilibiliUserCardData(outcome.Action, item, user, outcome.Services)
		return sendBilibiliUserCard(ctx, event, data, outcome.Message)
	case "weibo":
		user := weiboUser{UID: item.UID, Name: item.Name, AvatarURL: item.AvatarURL}
		if outcome.WeiboUser != nil {
			user = *outcome.WeiboUser
		}
		user.AvatarURL = inlineWeiboCardAvatar(ctx, event.Actions(), firstText(user.AvatarURL, item.AvatarURL))
		data := buildWeiboUserCardData(outcome.Action, item, user, outcome.Services)
		return sendRenderedCard(ctx, event, weiboUserCardTemplate, data, outcome.Message)
	}
	return event.SendText(outcome.Message)
}

// replySubscriptionCandidates 回复昵称无精确匹配的场景：提示文字走非终态的 message.send，
// 搜索结果图片占用事件的终态回复（每个事件只允许一次终态回复）；图片失败时以 result 收尾。
func replySubscriptionCandidates(ctx context.Context, event *rayleabot.EventContext, platform string, outcome subscriptionOutcome) error {
	if _, err := event.Actions().MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: normalizedTargetType(event.Event.Target.Type),
		TargetID:   event.Event.Target.ID,
		Message:    rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(outcome.Message)}},
	}); err != nil {
		// 非终态发送失败时退化为终态文字回复。
		return event.SendText(outcome.Message)
	}
	var data map[string]any
	template := ""
	switch platform {
	case "bilibili":
		if len(outcome.BilibiliCandidates) > 0 {
			renderUsers := prepareBilibiliSearchAvatars(ctx, event.Actions(), outcome.BilibiliCandidates)
			data = buildBilibiliSearchCardData(outcome.CandidatesQuery, renderUsers, event.CommandPrefixes)
			template = bilibiliSearchResultsTemplate
		}
	case "weibo":
		if len(outcome.WeiboCandidates) > 0 {
			renderUsers := prepareWeiboSearchAvatars(ctx, event.Actions(), outcome.WeiboCandidates)
			data = buildWeiboSearchCardData(outcome.CandidatesQuery, renderUsers, event.CommandPrefixes)
			template = weiboSearchResultsTemplate
		}
	}
	if template == "" {
		return event.Result(map[string]any{"handled": true, "card": false})
	}
	imagePath, err := renderBilibiliCardImage(ctx, event.Actions(), template, data, outcome.Message)
	if err != nil || imagePath == "" {
		return event.Result(map[string]any{"handled": true, "card": false})
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func commandOperation(command string) (string, string) {
	switch command {
	case "订阅状态":
		return "", "status"
	case "订阅b站推送":
		return "bilibili", "add"
	case "取消b站推送":
		return "bilibili", "remove"
	case "订阅微博推送":
		return "weibo", "add"
	case "取消微博推送":
		return "weibo", "remove"
	case "订阅抖音推送":
		return "douyin", "add"
	case "取消抖音推送":
		return "douyin", "remove"
	case "订阅网易云音乐推送":
		return "netease_music", "add"
	case "取消网易云音乐推送":
		return "netease_music", "remove"
	case "b站搜索up", "b站搜索UP", "B站搜索up", "B站搜索UP":
		return "bilibili", "search"
	case "微博搜索博主":
		return "weibo", "search"
	case "订阅列表":
		return "", "list"
	case "b站订阅列表":
		return "bilibili", "list"
	case "微博订阅列表":
		return "weibo", "list"
	case "抖音订阅列表":
		return "douyin", "list"
	case "网易云音乐订阅列表":
		return "netease_music", "list"
	case "全部订阅列表":
		return "", "list_all"
	case "全部b站订阅列表":
		return "bilibili", "list_all"
	case "全部微博订阅列表":
		return "weibo", "list_all"
	case "全部抖音订阅列表":
		return "douyin", "list_all"
	case "全部网易云音乐订阅列表":
		return "netease_music", "list_all"
	case "立即检查订阅":
		return "", "check"
	case "预览订阅卡片":
		return "", "preview"
	default:
		return "", ""
	}
}

func handleManagementAction(ctx context.Context, event *rayleabot.EventContext) error {
	action, _ := event.Event.Payload["action"].(string)
	payload, _ := event.Event.Payload["payload"].(map[string]any)
	cancel := func() {}
	if strings.TrimSpace(action) == "subscription.check_now" {
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
	}
	defer cancel()
	current, err := loadSettings(ctx, event)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(action) {
	case "subscription.check_now":
		return event.Result(checkSubscriptions(ctx, event, current))
	case "subscription.resolve_avatars":
		return event.Result(resolveAvatarDataURLs(ctx, event.Actions(), payload))
	case "subscription.resolve_user":
		platform := stringValue(payload, "platform", "bilibili")
		query := stringValue(payload, "query", "")
		if query == "" {
			return event.Result(map[string]any{"platform": platform, "query": query, "exact": false, "candidates": []any{}})
		}
		if platform == "bilibili" {
			users, resolveErr := resolveBilibiliUsers(ctx, event, query)
			if resolveErr != nil || len(users) == 0 {
				return event.Result(map[string]any{"platform": platform, "query": query, "exact": false, "candidates": []any{}, "message": friendlyBilibiliError(resolveErr)})
			}
			return event.Result(bilibiliManagementResolution(query, users))
		}
		if platform == "weibo" {
			users, resolveErr := resolveWeiboUsers(ctx, event, query)
			if resolveErr != nil || len(users) == 0 {
				return event.Result(map[string]any{"platform": platform, "query": query, "exact": false, "candidates": []any{}, "message": friendlyWeiboError(resolveErr)})
			}
			return event.Result(weiboManagementResolution(query, users))
		}
		uid := subjectIDFromInput(platform, query)
		if uid == "" {
			uid = safeSubjectID(query)
		}
		name := strings.TrimSpace(query)
		if name == "" {
			name = uid
		}
		user := map[string]any{"uid": uid, "name": name, "avatar_url": ""}
		return event.Result(map[string]any{"platform": platform, "query": query, "exact": uid != "", "user": user, "candidates": []any{user}})
	default:
		return event.Result(map[string]any{"handled": false, "message": "未知订阅中心管理动作。"})
	}
}

func bilibiliManagementResolution(query string, users []bilibiliUser) map[string]any {
	result := map[string]any{"platform": "bilibili", "query": query, "exact": false, "candidates": users}
	if matched := matchBilibiliUserByQuery(users, query); matched != nil {
		result["exact"] = true
		result["user"] = *matched
	}
	return result
}

func weiboManagementResolution(query string, users []weiboUser) map[string]any {
	result := map[string]any{"platform": "weibo", "query": query, "exact": false, "candidates": users}
	if matched := matchWeiboUserByQuery(users, query); matched != nil {
		result["exact"] = true
		result["user"] = *matched
	}
	return result
}

func loadSettings(ctx context.Context, event *rayleabot.EventContext) (settings, error) {
	current := settings{Enabled: true, DeliveryMaxAgeMinutes: defaultDeliveryMaxAgeMinutes, Subscriptions: []subscription{}}
	_ = json.Unmarshal(assets.DefaultConfigJSON, &current)
	result, err := event.Actions().ConfigRead(ctx, "enabled", "delivery_max_age_minutes", "subscriptions")
	if err != nil {
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
			Level: "warn", Message: "订阅设置读取失败，使用默认设置", Fields: map[string]any{"error": err.Error()},
		})
		ensureScheduler(ctx, event)
		return current, nil
	}
	values, _ := result["values"].(map[string]any)
	if enabled, ok := values["enabled"].(bool); ok {
		current.Enabled = enabled
	}
	if raw, ok := values["delivery_max_age_minutes"]; ok {
		current.DeliveryMaxAgeMinutes = int(intScalar(raw))
	}
	if raw, ok := values["subscriptions"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &current.Subscriptions)
	}
	current.DeliveryMaxAgeMinutes = normalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes)
	current.Subscriptions = normalizeSubscriptions(current.Subscriptions)
	normalizeSubscriptionSubscribers(current.Subscriptions, event.SuperAdmins)
	ensureScheduler(ctx, event)
	return current, nil
}

func ensureScheduler(ctx context.Context, event *rayleabot.EventContext) bool {
	if schedulerRegistered.Load() {
		return true
	}
	_, err := event.Actions().SchedulerCreate(ctx, rayleabot.SchedulerCreateRequest{
		TaskID: schedulerTaskID, Cron: schedulerCron, EventType: "scheduler.trigger",
		LogLabel: "订阅检查", Payload: map[string]any{"action": "check_subscriptions"},
	})
	if err != nil {
		_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
			Level: "warn", Message: "订阅检查任务注册失败", Fields: map[string]any{"error": err.Error()},
		})
		return false
	}
	schedulerRegistered.Store(true)
	_, _ = event.Actions().LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "info", Message: fmt.Sprintf("订阅中心插件创建定时任务订阅检查（%s）", schedulerCron),
		Fields: map[string]any{"task_id": schedulerTaskID, "cron": schedulerCron, "log_label": "订阅检查"},
	})
	return true
}

// schedulerJitterDelay 返回 0-20s 的调度抖动。cron 触发是整分钟对齐的，
// 所有实例/插件在同一秒内开始检查；随机延迟把上游请求摊开。
func schedulerJitterDelay() time.Duration {
	return time.Duration(rand.IntN(21)) * time.Second
}

func saveSettings(ctx context.Context, event *rayleabot.EventContext, current settings) error {
	_, err := event.Actions().ConfigWrite(ctx, map[string]any{
		"enabled":                  current.Enabled,
		"delivery_max_age_minutes": normalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes),
		"subscriptions":            current.Subscriptions,
	})
	return err
}

func normalizeDeliveryMaxAgeMinutes(value int) int {
	if value < minimumDeliveryMaxAgeMinutes {
		return defaultDeliveryMaxAgeMinutes
	}
	if value > maximumDeliveryMaxAgeMinutes {
		return maximumDeliveryMaxAgeMinutes
	}
	return value
}

type subscriptionOutcome struct {
	Message            string
	Changed            bool
	Action             string
	Item               *subscription
	User               *bilibiliUser
	WeiboUser          *weiboUser
	Services           []string
	CandidatesQuery    string
	BilibiliCandidates []bilibiliUser
	WeiboCandidates    []weiboUser
}

func addSubscription(ctx context.Context, current *settings, event *rayleabot.EventContext, platform string) subscriptionOutcome {
	return addSubscriptionWithActions(ctx, event.Actions(), current, event, platform)
}

func addSubscriptionWithActions(ctx context.Context, actions pluginActions, current *settings, event *rayleabot.EventContext, platform string) subscriptionOutcome {
	services, query, ok := parseSubscriptionArgs(event.Event.Args(), platform)
	if !ok {
		return subscriptionOutcome{Message: "请填写要订阅的账号 ID 或主页标识。"}
	}
	uid := subjectIDFromInput(platform, query)
	name := strings.TrimSpace(query)
	avatarURL := ""
	var resolvedUser *bilibiliUser
	var resolvedWeiboUser *weiboUser
	if platform == "bilibili" {
		users, err := resolveBilibiliUsersWithActions(ctx, actions, query)
		if err != nil || len(users) == 0 {
			return subscriptionOutcome{Message: friendlyBilibiliError(err)}
		}
		matched := matchBilibiliUserByQuery(users, query)
		if matched == nil {
			return subscriptionOutcome{
				Message:            "没有找到昵称与「" + query + "」完全一致的 Bilibili UP 主。可以参考下面的搜索结果，用更准确的昵称或 UID 重新订阅。",
				Action:             "candidates",
				CandidatesQuery:    query,
				BilibiliCandidates: users,
			}
		}
		uid, name, avatarURL = matched.UID, matched.Name, matched.AvatarURL
		resolvedUser = matched
	} else if platform == "weibo" {
		users, err := resolveWeiboUsersWithActions(ctx, actions, query)
		if err == nil && len(users) > 0 {
			matched := matchWeiboUserByQuery(users, query)
			if matched == nil {
				return subscriptionOutcome{
					Message:         "没有找到昵称与「" + query + "」完全一致的微博博主。可以参考下面的搜索结果，用更准确的昵称或 UID 重新订阅。",
					Action:          "candidates",
					CandidatesQuery: query,
					WeiboCandidates: users,
				}
			}
			uid, name, avatarURL = matched.UID, matched.Name, matched.AvatarURL
			resolvedWeiboUser = matched
		} else if explicitUID := weiboUIDFromInput(query); explicitUID != "" {
			// 显式 UID/主页链接在联网解析失败时保留本地订阅行为。
			uid, name = explicitUID, explicitUID
		} else {
			return subscriptionOutcome{Message: friendlyWeiboError(err)}
		}
	} else {
		if uid == "" {
			uid = safeSubjectID(query)
		}
		if name == "" {
			name = uid
		}
	}
	if uid == "" || event.Event.Target.ID == "" {
		return subscriptionOutcome{Message: "当前会话无法绑定订阅目标。"}
	}
	targetType := event.Event.Target.Type
	if targetType != "private" {
		targetType = "group"
	}
	id := subscriptionID(platform, uid, targetType, event.Event.Target.ID)
	for index := range current.Subscriptions {
		item := &current.Subscriptions[index]
		if item.ID != id {
			continue
		}
		item.Enabled = true
		item.Name = name
		if avatarURL != "" {
			item.AvatarURL = avatarURL
		}
		if targetName := currentTargetName(event); targetName != "" {
			item.TargetName = targetName
		}
		item.Services = mergeServices(item.Services, services, platform)
		item.Subscribers = mergeSubscriber(item.Subscribers, event)
		return subscriptionOutcome{
			Message:   "已更新订阅：" + platformName(platform) + " " + item.Name + "（" + servicesText(item.Services, platform) + "）",
			Changed:   true,
			Action:    "updated",
			Item:      item,
			User:      resolvedUser,
			WeiboUser: resolvedWeiboUser,
			Services:  item.Services,
		}
	}
	current.Subscriptions = append(current.Subscriptions, subscription{
		ID: id, Platform: platform, UID: uid, Name: name, AvatarURL: avatarURL, TargetType: targetType, TargetID: event.Event.Target.ID,
		TargetName: currentTargetName(event), Services: services, Subscribers: mergeSubscriber(nil, event), Enabled: true,
	})
	return subscriptionOutcome{
		Message:   "已订阅：" + platformName(platform) + " " + name + "（" + servicesText(services, platform) + "）",
		Changed:   true,
		Action:    "subscribed",
		Item:      &current.Subscriptions[len(current.Subscriptions)-1],
		User:      resolvedUser,
		WeiboUser: resolvedWeiboUser,
		Services:  services,
	}
}

func removeSubscription(current *settings, event *rayleabot.EventContext, platform string) subscriptionOutcome {
	services, query, ok := parseSubscriptionArgs(event.Event.Args(), platform)
	if !ok {
		return subscriptionOutcome{Message: "请填写要取消的账号 ID 或主页标识。"}
	}
	uid := subjectIDFromInput(platform, query)
	if uid == "" {
		uid = safeSubjectID(query)
	}
	remaining := make([]subscription, 0, len(current.Subscriptions))
	removed := false
	var removedItem *subscription
	for _, item := range current.Subscriptions {
		matchesSubject := item.UID == uid || strings.EqualFold(item.Name, strings.TrimSpace(query))
		if item.Platform == platform && matchesSubject && item.TargetType == normalizedTargetType(event.Event.Target.Type) && item.TargetID == event.Event.Target.ID {
			matched := item
			removedItem = &matched
			nextServices := removeServices(item.Services, services, platform)
			removed = true
			if len(nextServices) == 0 {
				continue
			}
			item.Services = nextServices
		}
		remaining = append(remaining, item)
	}
	if !removed {
		return subscriptionOutcome{Message: "当前会话没有这项订阅。"}
	}
	current.Subscriptions = remaining
	return subscriptionOutcome{
		Message:  "已取消订阅：" + platformName(platform) + " " + query + "（" + servicesText(services, platform) + "）",
		Changed:  true,
		Action:   "unsubscribed",
		Item:     removedItem,
		Services: services,
	}
}

func normalizeSubscriptions(items []subscription) []subscription {
	seen := map[string]bool{}
	result := make([]subscription, 0, len(items))
	for _, item := range items {
		item.Platform = normalizePlatform(item.Platform)
		item.UID = strings.TrimSpace(item.UID)
		item.TargetType = normalizedTargetType(item.TargetType)
		item.TargetID = strings.TrimSpace(item.TargetID)
		if item.Platform == "" || item.UID == "" || item.TargetID == "" {
			continue
		}
		if item.ID == "" {
			item.ID = subscriptionID(item.Platform, item.UID, item.TargetType, item.TargetID)
		}
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		if item.Name == "" {
			item.Name = item.UID
		}
		item.Services = normalizeServices(item.Services, item.Platform)
		result = append(result, item)
	}
	return result
}

func formatStatus(current settings) string {
	enabled := 0
	for _, item := range current.Subscriptions {
		if item.Enabled {
			enabled++
		}
	}
	state := "停用"
	if current.Enabled {
		state = "启用"
	}
	return fmt.Sprintf("订阅中心\n状态：%s\n订阅：%d/%d\n平台：Bilibili、微博、抖音、网易云音乐\n检查：订阅中心插件定时检查，支持手动立即检查\n账号：Web 三方账号页面管理平台 Cookie", state, enabled, len(current.Subscriptions))
}

func formatSubscriptions(current settings, event *rayleabot.EventContext, platform string, all bool) string {
	items := make([]subscription, 0)
	for _, item := range current.Subscriptions {
		if platform != "" && item.Platform != platform {
			continue
		}
		if !all && (item.TargetType != normalizedTargetType(event.Event.Target.Type) || item.TargetID != event.Event.Target.ID) {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return subscriptionListTitle(platform, all) + "\n当前没有订阅。"
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	lines := []string{subscriptionListTitle(platform, all)}
	for _, item := range items {
		targetLabel := "群聊"
		if item.TargetType == "private" {
			targetLabel = "私聊"
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s %s · %s · 订阅人：%s", targetLabel, item.TargetID,
			platformName(item.Platform), subscriptionSubjectText(item), servicesText(item.Services, item.Platform), subscribersText(item)))
	}
	return strings.Join(lines, "\n")
}

func subscriptionListTitle(platform string, all bool) string {
	if platform == "" {
		if all {
			return "全部订阅列表"
		}
		return "订阅列表"
	}
	name := platformName(platform)
	if platform == "bilibili" {
		name = "Bilibili"
		if all {
			return "全部 Bilibili 订阅列表"
		}
		return "Bilibili 订阅列表"
	}
	if all {
		return "全部" + name + "订阅列表"
	}
	return name + "订阅列表"
}

func subscriptionSubjectText(item subscription) string {
	name, uid := strings.TrimSpace(item.Name), strings.TrimSpace(item.UID)
	if name == "" || name == uid {
		return firstText(uid, name)
	}
	label := map[string]string{"bilibili": "UID", "weibo": "UID", "douyin": "抖音号", "netease_music": "ID"}[item.Platform]
	return fmt.Sprintf("%s（%s %s）", name, label, uid)
}

func subscribersText(item subscription) string {
	names := make([]string, 0, len(item.Subscribers))
	for _, subscriber := range item.Subscribers {
		if name := firstText(subscriber.Nickname, subscriber.ID); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "未记录"
	}
	return strings.Join(names, "、")
}

func currentTargetName(event *rayleabot.EventContext) string {
	name := strings.TrimSpace(event.Event.Target.Name)
	onebot := mapValue(event.Event.Payload["onebot"])
	sender := mapValue(onebot["sender"])
	if name == "" && normalizedTargetType(event.Event.Target.Type) == "group" {
		name = stringScalar(onebot["group_name"])
	}
	if name == "" && normalizedTargetType(event.Event.Target.Type) == "private" {
		name = firstText(event.Event.Actor.Nickname, sender["nickname"])
	}
	if name == event.Event.Target.ID {
		return ""
	}
	return name
}

func bilibiliDynamicService(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DYNAMIC_TYPE_AV":
		return "video"
	case "DYNAMIC_TYPE_ARTICLE":
		return "article"
	case "DYNAMIC_TYPE_FORWARD":
		return "repost"
	default:
		return "image_text"
	}
}

func mergeSubscriber(items []subscriber, event *rayleabot.EventContext) []subscriber {
	onebot := mapValue(event.Event.Payload["onebot"])
	sender := mapValue(onebot["sender"])
	id := firstText(event.Event.Actor.ID, sender["user_id"], onebot["user_id"])
	if id == "" {
		return items
	}
	actorRole := strings.ToLower(strings.TrimSpace(event.Event.Actor.Role))
	senderRole := strings.ToLower(strings.TrimSpace(stringScalar(sender["role"])))
	baseRole := strongestSubscriberBaseRole(actorRole, senderRole)
	role := strongestSubscriberRole(actorRole, senderRole)
	for _, superAdmin := range event.SuperAdmins {
		if strings.TrimSpace(superAdmin) == id {
			role = "super_admin"
			break
		}
	}
	next := subscriber{
		ID: id, Nickname: firstText(event.Event.Actor.Nickname, sender["nickname"], id),
		GroupNickname: stringScalar(sender["card"]), Title: stringScalar(sender["title"]),
		BaseRole: baseRole, Role: role, RoleLabel: subscriberRoleLabel(role), AvatarURL: qqAvatarURL(id),
	}
	for index := range items {
		if items[index].ID == id {
			items[index] = mergeSubscriberIdentity(items[index], next)
			return items
		}
	}
	return append(items, next)
}

func normalizeSubscriptionSubscribers(items []subscription, superAdmins []string) {
	adminIDs := make(map[string]struct{}, len(superAdmins))
	for _, id := range superAdmins {
		if id = strings.TrimSpace(id); id != "" {
			adminIDs[id] = struct{}{}
		}
	}
	for itemIndex := range items {
		for subscriberIndex := range items[itemIndex].Subscribers {
			current := &items[itemIndex].Subscribers[subscriberIndex]
			current.ID = strings.TrimSpace(current.ID)
			current.Nickname = firstText(current.Nickname, current.ID)
			current.GroupNickname = strings.TrimSpace(current.GroupNickname)
			current.Title = strings.TrimSpace(current.Title)
			current.BaseRole = strings.ToLower(strings.TrimSpace(current.BaseRole))
			current.Role = strings.ToLower(strings.TrimSpace(current.Role))
			if current.BaseRole == "super_admin" {
				current.BaseRole = ""
			}
			if current.BaseRole == "" && current.Role != "super_admin" {
				current.BaseRole = current.Role
			}
			if _, ok := adminIDs[current.ID]; ok {
				current.Role = "super_admin"
			} else if current.Role == "super_admin" {
				current.Role = firstText(current.BaseRole, "member")
			}
			if label := subscriberRoleLabel(current.Role); label != "" {
				current.RoleLabel = label
			} else {
				current.RoleLabel = strings.TrimSpace(current.RoleLabel)
			}
			if avatar := qqAvatarURL(current.ID); avatar != "" {
				current.AvatarURL = avatar
			} else {
				current.AvatarURL = strings.TrimSpace(current.AvatarURL)
			}
		}
	}
}

func mergeSubscriberIdentity(current, incoming subscriber) subscriber {
	current.ID = firstText(incoming.ID, current.ID)
	current.Nickname = firstText(incoming.Nickname, current.Nickname, current.ID)
	current.GroupNickname = firstText(incoming.GroupNickname, current.GroupNickname)
	current.Title = firstText(incoming.Title, current.Title)
	current.BaseRole = firstText(incoming.BaseRole, current.BaseRole)
	current.Role = firstText(incoming.Role, current.Role)
	current.RoleLabel = firstText(subscriberRoleLabel(current.Role), incoming.RoleLabel, current.RoleLabel)
	current.AvatarURL = firstText(qqAvatarURL(current.ID), incoming.AvatarURL, current.AvatarURL)
	return current
}

func strongestSubscriberRole(values ...string) string {
	ranks := map[string]int{"member": 1, "admin": 2, "owner": 3, "super_admin": 4}
	strongest := ""
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if ranks[value] > ranks[strongest] {
			strongest = value
		}
	}
	return strongest
}

func strongestSubscriberBaseRole(values ...string) string {
	ranks := map[string]int{"member": 1, "admin": 2, "owner": 3}
	strongest := ""
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if ranks[value] > ranks[strongest] {
			strongest = value
		}
	}
	return strongest
}

func subscriptionID(platform, uid, targetType, targetID string) string {
	value := strings.Join([]string{platform, uid, targetType, targetID}, "|")
	digest := sha256.Sum256([]byte(value))
	return platform + "-" + hex.EncodeToString(digest[:8])
}

func normalizePlatform(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bilibili", "weibo", "douyin", "netease_music":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizedTargetType(value string) string {
	if strings.TrimSpace(value) == "private" {
		return "private"
	}
	return "group"
}

func platformName(platform string) string {
	return map[string]string{"bilibili": "Bilibili", "weibo": "微博", "douyin": "抖音", "netease_music": "网易云音乐"}[platform]
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
