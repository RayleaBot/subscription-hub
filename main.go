package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	schedulerTaskID = "subscription-hub-check"
	schedulerCron   = "*/1 * * * *"
)

//go:embed default_config.json
var defaultConfigJSON []byte

type settings struct {
	Enabled       bool           `json:"enabled"`
	Subscriptions []subscription `json:"subscriptions"`
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
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Role     string `json:"role,omitempty"`
}

type bilibiliFeed struct {
	Code int `json:"code"`
	Data struct {
		Items []struct {
			ID      string `json:"id_str"`
			Type    string `json:"type"`
			Modules struct {
				Author struct {
					Name string `json:"name"`
				} `json:"module_author"`
				Dynamic struct {
					Description struct {
						Text string `json:"text"`
					} `json:"desc"`
				} `json:"module_dynamic"`
			} `json:"modules"`
		} `json:"items"`
	} `json:"data"`
}

type bilibiliLiveStatus struct {
	Code int `json:"code"`
	Data map[string]struct {
		UID        int64  `json:"uid"`
		UName      string `json:"uname"`
		Title      string `json:"title"`
		RoomID     int64  `json:"room_id"`
		LiveStatus int    `json:"live_status"`
		LiveTime   int64  `json:"live_time"`
	} `json:"data"`
}

func main() {
	err := rayleabot.Run(context.Background(), rayleabot.Options{
		PluginID: "raylea.subscription-hub",
		Subscriptions: []string{
			"message.group", "message.private", "plugin.started", "config.changed", "scheduler.trigger", "management.action",
		},
		MaxConcurrentHandlers: 1,
	}, rayleabot.HandlerFunc(handleEvent))
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func handleEvent(ctx context.Context, event *rayleabot.EventContext) error {
	switch event.Event.EventType {
	case "plugin.started":
		_, err := event.Actions().SchedulerCreate(ctx, rayleabot.SchedulerCreateRequest{
			TaskID: schedulerTaskID, Cron: schedulerCron, EventType: "scheduler.trigger",
			LogLabel: "订阅检查", Payload: map[string]any{"action": "check_subscriptions"},
		})
		if err != nil {
			return err
		}
		return event.Result(map[string]any{"handled": true, "scheduler_registered": true})
	case "config.changed":
		return event.Result(map[string]any{"handled": true, "reloaded": true})
	case "scheduler.trigger":
		current, err := loadSettings(ctx, event)
		if err != nil {
			return err
		}
		result := checkSubscriptions(ctx, event, current)
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
	current, err := loadSettings(ctx, event)
	if err != nil {
		return err
	}
	platform, operation := commandOperation(command)
	switch operation {
	case "status":
		return event.SendText(formatStatus(current))
	case "add":
		message, changed := addSubscription(ctx, &current, event, platform)
		if changed {
			if err := saveSettings(ctx, event, current); err != nil {
				return err
			}
		}
		return event.SendText(message)
	case "remove":
		message, changed := removeSubscription(&current, event, platform)
		if changed {
			if err := saveSettings(ctx, event, current); err != nil {
				return err
			}
		}
		return event.SendText(message)
	case "list", "list_all":
		return event.SendText(formatSubscriptions(current, event, platform, operation == "list_all"))
	case "check":
		result := checkSubscriptions(ctx, event, current)
		return event.SendText(fmt.Sprintf("订阅检查完成：检查 %v 项，推送 %v 项，失败 %v 项。", result["checked"], result["pushed"], result["failed"]))
	case "search":
		return event.SendText(searchBilibiliUsers(ctx, event, strings.Join(event.Event.Args(), " ")))
	case "preview":
		return previewSubscriptionCard(ctx, event, strings.Join(event.Event.Args(), " "))
	default:
		return event.Result(map[string]any{"handled": false})
	}
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
	case "b站搜索up":
		return "bilibili", "search"
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
		return "bilibili", "preview"
	default:
		return "", ""
	}
}

func handleManagementAction(ctx context.Context, event *rayleabot.EventContext) error {
	action, _ := event.Event.Payload["action"].(string)
	payload, _ := event.Event.Payload["payload"].(map[string]any)
	current, err := loadSettings(ctx, event)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(action) {
	case "subscription.check_now":
		return event.Result(checkSubscriptions(ctx, event, current))
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
			return event.Result(map[string]any{"platform": platform, "query": query, "exact": len(users) == 1, "user": users[0], "candidates": users})
		}
		uid := subjectIDFromInput(platform, query)
		if uid == "" {
			uid = safeSubjectID(query)
		}
		user := map[string]any{"uid": uid, "name": uid, "avatar_url": ""}
		return event.Result(map[string]any{"platform": platform, "query": query, "exact": uid != "", "user": user, "candidates": []any{user}})
	default:
		return event.Result(map[string]any{"handled": false, "message": "未知订阅中心管理动作。"})
	}
}

func loadSettings(ctx context.Context, event *rayleabot.EventContext) (settings, error) {
	current := settings{Enabled: true, Subscriptions: []subscription{}}
	_ = json.Unmarshal(defaultConfigJSON, &current)
	result, err := event.Actions().ConfigRead(ctx, "enabled", "subscriptions")
	if err != nil {
		return settings{}, err
	}
	values, _ := result["values"].(map[string]any)
	if enabled, ok := values["enabled"].(bool); ok {
		current.Enabled = enabled
	}
	if raw, ok := values["subscriptions"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &current.Subscriptions)
	}
	current.Subscriptions = normalizeSubscriptions(current.Subscriptions)
	return current, nil
}

func saveSettings(ctx context.Context, event *rayleabot.EventContext, current settings) error {
	_, err := event.Actions().ConfigWrite(ctx, map[string]any{"enabled": current.Enabled, "subscriptions": current.Subscriptions})
	return err
}

func addSubscription(ctx context.Context, current *settings, event *rayleabot.EventContext, platform string) (string, bool) {
	services, query, ok := parseSubscriptionArgs(event.Event.Args(), platform)
	if !ok {
		return "请填写要订阅的账号 ID 或主页标识。", false
	}
	uid := subjectIDFromInput(platform, query)
	name := uid
	avatarURL := ""
	if platform == "bilibili" {
		users, err := resolveBilibiliUsers(ctx, event, query)
		if err != nil || len(users) == 0 {
			return friendlyBilibiliError(err), false
		}
		uid, name, avatarURL = users[0].UID, users[0].Name, users[0].AvatarURL
	} else if uid == "" {
		uid = safeSubjectID(query)
		name = uid
	}
	if uid == "" || event.Event.Target.ID == "" {
		return "当前会话无法绑定订阅目标。", false
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
		item.Services = mergeServices(item.Services, services, platform)
		item.Subscribers = mergeSubscriber(item.Subscribers, event)
		return "已更新订阅：" + platformName(platform) + " " + item.Name + "（" + servicesText(item.Services, platform) + "）", true
	}
	current.Subscriptions = append(current.Subscriptions, subscription{
		ID: id, Platform: platform, UID: uid, Name: name, AvatarURL: avatarURL, TargetType: targetType, TargetID: event.Event.Target.ID,
		TargetName: event.Event.Target.Name, Services: services, Subscribers: mergeSubscriber(nil, event), Enabled: true,
	})
	return "已订阅：" + platformName(platform) + " " + name + "（" + servicesText(services, platform) + "）", true
}

func removeSubscription(current *settings, event *rayleabot.EventContext, platform string) (string, bool) {
	services, query, ok := parseSubscriptionArgs(event.Event.Args(), platform)
	if !ok {
		return "请填写要取消的账号 ID 或主页标识。", false
	}
	uid := subjectIDFromInput(platform, query)
	if uid == "" {
		uid = safeSubjectID(query)
	}
	remaining := make([]subscription, 0, len(current.Subscriptions))
	removed := false
	for _, item := range current.Subscriptions {
		matchesSubject := item.UID == uid || strings.EqualFold(item.Name, strings.TrimSpace(query))
		if item.Platform == platform && matchesSubject && item.TargetType == normalizedTargetType(event.Event.Target.Type) && item.TargetID == event.Event.Target.ID {
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
		return "当前会话没有这项订阅。", false
	}
	current.Subscriptions = remaining
	return "已取消订阅：" + platformName(platform) + " " + query + "（" + servicesText(services, platform) + "）", true
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
	return fmt.Sprintf("订阅中心\n状态：%s\n订阅：%d/%d\n平台：Bilibili、微博、抖音、网易云音乐", state, enabled, len(current.Subscriptions))
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
		return "订阅列表\n当前没有订阅。"
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	lines := []string{"订阅列表"}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("%s %s（%s）→ %s %s", platformName(item.Platform), item.Name, servicesText(item.Services, item.Platform), item.TargetType, item.TargetID))
	}
	return strings.Join(lines, "\n")
}

func checkSubscriptions(ctx context.Context, event *rayleabot.EventContext, current settings) map[string]any {
	checked, pushed, failed := 0, 0, 0
	if !current.Enabled {
		return map[string]any{"handled": true, "checked": 0, "pushed": 0, "failed": 0, "disabled": true}
	}
	for _, item := range current.Subscriptions {
		if !item.Enabled || item.Platform != "bilibili" {
			continue
		}
		checked++
		changed, message, err := checkBilibili(ctx, event, item)
		if err != nil {
			failed++
			continue
		}
		if !changed {
			continue
		}
		_, err = event.Actions().MessageSend(ctx, rayleabot.MessageSendRequest{
			TargetType: item.TargetType, TargetID: item.TargetID,
			Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(message)}},
		})
		if err != nil {
			failed++
			continue
		}
		pushed++
	}
	return map[string]any{"handled": true, "checked": checked, "pushed": pushed, "failed": failed}
}

func checkBilibili(ctx context.Context, event *rayleabot.EventContext, item subscription) (bool, string, error) {
	dynamicEnabled := serviceEnabled(item, "video") || serviceEnabled(item, "image_text") || serviceEnabled(item, "article") || serviceEnabled(item, "repost")
	if dynamicEnabled {
		changed, message, err := checkBilibiliDynamic(ctx, event, item)
		if err != nil || changed {
			return changed, message, err
		}
	}
	if serviceEnabled(item, "live") {
		return checkBilibiliLive(ctx, event, item)
	}
	return false, "", nil
}

func checkBilibiliDynamic(ctx context.Context, event *rayleabot.EventContext, item subscription) (bool, string, error) {
	endpoint := "https://api.bilibili.com/x/polymer/web-dynamic/v1/feed/space?host_mid=" + url.QueryEscape(item.UID)
	result, err := event.Actions().HTTPRequest(ctx, rayleabot.HTTPRequest{Method: "GET", URL: endpoint, TimeoutSeconds: 15, Headers: bilibiliHeaders(readBilibiliCookie(ctx, event), item.UID)})
	if err != nil {
		return false, "", err
	}
	body, _ := result["body_text"].(string)
	var feed bilibiliFeed
	if json.Unmarshal([]byte(body), &feed) != nil || feed.Code != 0 || len(feed.Data.Items) == 0 {
		return false, "", fmt.Errorf("invalid Bilibili feed response")
	}
	latest := feed.Data.Items[0]
	if latest.ID == "" {
		return false, "", fmt.Errorf("Bilibili feed has no item id")
	}
	cursorKey := "cursor:" + item.ID
	cursor, _ := event.Actions().KVGet(ctx, cursorKey)
	previous, _ := cursor["value"].(string)
	if _, err := event.Actions().KVSet(ctx, cursorKey, latest.ID); err != nil {
		return false, "", err
	}
	if previous == "" || previous == latest.ID {
		return false, "", nil
	}
	service := bilibiliDynamicService(latest.Type)
	if !serviceEnabled(item, service) {
		return false, "", nil
	}
	author := latest.Modules.Author.Name
	if author == "" {
		author = item.Name
	}
	description := strings.TrimSpace(latest.Modules.Dynamic.Description.Text)
	if description == "" {
		description = "发布了新动态"
	}
	return true, fmt.Sprintf("%s 更新\n%s\nhttps://t.bilibili.com/%s", author, description, latest.ID), nil
}

func checkBilibiliLive(ctx context.Context, event *rayleabot.EventContext, item subscription) (bool, string, error) {
	endpoint := "https://api.live.bilibili.com/room/v1/Room/get_status_info_by_uids?uids[]=" + url.QueryEscape(item.UID)
	result, err := event.Actions().HTTPRequest(ctx, rayleabot.HTTPRequest{Method: "GET", URL: endpoint, TimeoutSeconds: 15, Headers: bilibiliHeaders(readBilibiliCookie(ctx, event), item.UID)})
	if err != nil {
		return false, "", err
	}
	body, _ := result["body_text"].(string)
	var response bilibiliLiveStatus
	if json.Unmarshal([]byte(body), &response) != nil || response.Code != 0 {
		return false, "", fmt.Errorf("invalid Bilibili live response")
	}
	entry, exists := response.Data[item.UID]
	if !exists {
		return false, "", nil
	}
	cursorValue := fmt.Sprintf("%d:%d:%d", entry.LiveStatus, entry.RoomID, entry.LiveTime)
	cursorKey := "live_cursor:" + item.ID
	cursor, _ := event.Actions().KVGet(ctx, cursorKey)
	previous, _ := cursor["value"].(string)
	if _, err := event.Actions().KVSet(ctx, cursorKey, cursorValue); err != nil {
		return false, "", err
	}
	if previous == "" || previous == cursorValue {
		return false, "", nil
	}
	author := strings.TrimSpace(entry.UName)
	if author == "" {
		author = item.Name
	}
	state := "已下播"
	if entry.LiveStatus == 1 {
		state = "开始直播"
	}
	return true, fmt.Sprintf("%s %s\n%s\nhttps://live.bilibili.com/%d", author, state, entry.Title, entry.RoomID), nil
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
	id := strings.TrimSpace(event.Event.Actor.ID)
	if id == "" {
		return items
	}
	next := subscriber{ID: id, Nickname: strings.TrimSpace(event.Event.Actor.Nickname), Role: strings.TrimSpace(event.Event.Actor.Role)}
	if next.Nickname == "" {
		next.Nickname = id
	}
	for index := range items {
		if items[index].ID == id {
			items[index] = next
			return items
		}
	}
	return append(items, next)
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
