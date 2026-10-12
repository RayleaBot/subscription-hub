package plugin

import (
	"context"
	"fmt"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type SubscriptionOutcome struct {
	Message         string
	Changed         bool
	Action          string
	Item            *Subscription
	User            *User
	Services        []string
	CandidatesQuery string
	Candidates      []User
	session         Session
}

func (handler *Handler) AddSubscription(ctx context.Context, actions SourceActions, current *Settings, event *rayleabot.EventContext, platform string) SubscriptionOutcome {
	definition, exists := handler.byID[platform]
	if !exists {
		return SubscriptionOutcome{Message: "不支持的订阅平台。"}
	}
	services, query, ok := definition.Services.ParseArgs(event.Event.Args())
	if !ok {
		return SubscriptionOutcome{Message: "请填写要订阅的账号 ID 或主页标识。"}
	}
	session := definition.NewSession(sourceBoundary(actions))
	resolution, err := session.Resolve(ctx, ResolveRequest{Query: query, Purpose: "subscribe", Subscriptions: current.Subscriptions})
	if err != nil {
		return SubscriptionOutcome{Message: failureText(platform, "resolve", err.Error())}
	}
	if resolution.Matched == nil {
		return SubscriptionOutcome{Action: "candidates", Message: resolution.Message, CandidatesQuery: query, Candidates: resolution.Candidates, session: session}
	}
	user := resolution.Matched
	if user.UID == "" || event.Event.Target.ID == "" {
		return SubscriptionOutcome{Message: "当前会话无法绑定订阅目标。"}
	}
	targetType := NormalizedTargetType(event.Event.Target.Type)
	if !validSubscriptionRoute(event.Event.SourceProtocol, event.Event.BotID) {
		return SubscriptionOutcome{Message: "请通过 OneBot11 机器人添加订阅。"}
	}
	id := SubscriptionID(platform, user.UID, event.Event.SourceProtocol, event.Event.BotID, targetType, event.Event.Target.ID)
	for index := range current.Subscriptions {
		item := &current.Subscriptions[index]
		if item.Platform != platform || item.UID != user.UID || !subscriptionTargetsEvent(*item, event) {
			continue
		}
		item.Enabled, item.Name = true, user.Name
		if user.AvatarURL != "" {
			item.AvatarURL = user.AvatarURL
		}
		if user.UniqueID != "" {
			item.UniqueID = user.UniqueID
		}
		if targetName := CurrentTargetName(event); targetName != "" {
			item.TargetName = targetName
		}
		item.Services = definition.Services.Merge(item.Services, services)
		item.Subscribers = MergeSubscriber(item.Subscribers, event)
		return SubscriptionOutcome{
			Message: "已更新订阅：" + definition.Name + " " + item.Name + "（" + definition.Services.Text(item.Services) + "）",
			Changed: true, Action: "updated", Item: item, User: user, Services: item.Services, session: session,
		}
	}
	current.Subscriptions = append(current.Subscriptions, Subscription{
		SourceProtocol: event.Event.SourceProtocol, BotID: event.Event.BotID,
		ID: id, Platform: platform, UID: user.UID, UniqueID: user.UniqueID, Name: user.Name, AvatarURL: user.AvatarURL,
		TargetType: targetType, TargetID: event.Event.Target.ID, TargetName: CurrentTargetName(event),
		Services: services, Subscribers: MergeSubscriber(nil, event), Enabled: true,
	})
	return SubscriptionOutcome{
		Message: "已订阅：" + definition.Name + " " + user.Name + "（" + definition.Services.Text(services) + "）",
		Changed: true, Action: "subscribed", Item: &current.Subscriptions[len(current.Subscriptions)-1], User: user, Services: services, session: session,
	}
}

func (handler *Handler) RemoveSubscription(current *Settings, event *rayleabot.EventContext, platform string) SubscriptionOutcome {
	definition, exists := handler.byID[platform]
	if !exists {
		return SubscriptionOutcome{Message: "不支持的订阅平台。"}
	}
	services, query, ok := definition.Services.ParseArgs(event.Event.Args())
	if !ok {
		return SubscriptionOutcome{Message: "请填写要取消的账号 ID 或主页标识。"}
	}
	uid := definition.ParseSubject(query)
	if uid == "" {
		uid = SafeSubjectID(query)
	}
	remaining := make([]Subscription, 0, len(current.Subscriptions))
	var removedItem *Subscription
	for _, item := range current.Subscriptions {
		matches := item.UID == uid || strings.EqualFold(item.Name, strings.TrimSpace(query))
		if item.Platform == platform && matches && subscriptionTargetsEvent(item, event) {
			matched := item
			removedItem = &matched
			item.Services = definition.Services.Remove(item.Services, services)
			if len(item.Services) == 0 {
				continue
			}
		}
		remaining = append(remaining, item)
	}
	if removedItem == nil {
		return SubscriptionOutcome{Message: "当前会话没有这项订阅。"}
	}
	current.Subscriptions = remaining
	return SubscriptionOutcome{
		Message: "已取消订阅：" + definition.Name + " " + query + "（" + definition.Services.Text(services) + "）",
		Changed: true, Action: "unsubscribed", Item: removedItem, Services: services,
	}
}

func (handler *Handler) ResolveUser(ctx context.Context, actions SourceActions, platform, query string, current Settings) map[string]any {
	result := Resolution{}
	definition, exists := handler.byID[platform]
	if !exists || query == "" {
		return result.ManagementResult(platform, query)
	}
	result, err := definition.NewSession(sourceBoundary(actions)).Resolve(ctx, ResolveRequest{Query: query, Purpose: "management", Subscriptions: current.Subscriptions})
	if err != nil {
		result.Message = failureText(platform, "resolve", err.Error())
	}
	return result.ManagementResult(platform, query)
}

func (handler *Handler) handleManagementAction(ctx context.Context, event *rayleabot.EventContext) error {
	action := strings.TrimSpace(StringScalar(event.Event.Payload["action"]))
	payload := MapValue(event.Event.Payload["payload"])
	switch action {
	case "subscription.save", "resolver.save", "strategy.save":
		result, err := handler.saveManagementSettings(ctx, event, action, payload)
		if err != nil {
			return event.Fail("platform.invalid_request", err.Error())
		}
		return event.Result(result)
	case "history.list", "history.assign", "history.delete":
		result, err := handler.manageHistory(ctx, event, action, payload)
		if err != nil {
			return event.Fail("platform.invalid_request", err.Error())
		}
		return event.Result(result)
	}
	var bot rayleabot.BotAccount
	switch action {
	case "subscription.settings", "resolver.settings", "subscription.check_now", "subscription.resolve_user":
		var err error
		bot, err = managementBot(event)
		if err != nil {
			return event.Fail("platform.invalid_request", err.Error())
		}
		if action != "subscription.resolve_user" && len(payload) != 0 {
			return event.Fail("platform.invalid_request", "此动作不接受业务参数")
		}
	case "strategy.settings":
		if err := globalManagement(event); err != nil {
			return event.Fail("platform.invalid_request", err.Error())
		}
	}
	if action == "subscription.check_now" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
	}
	current, err := handler.loadSettings(ctx, event)
	if err != nil {
		return err
	}
	if bot.BotID != "" {
		current = settingsForBot(current, bot)
	}
	actions := handler.hostActions(event)
	switch action {
	case "subscription.settings", "resolver.settings":
		return event.Result(map[string]any{"settings": current})
	case "strategy.settings":
		current.Subscriptions, current.Resolver.Targets = []Subscription{}, []ResolverTarget{}
		return event.Result(map[string]any{"settings": current})
	case "subscription.check_now":
		return event.Result(handler.Check(ctx, actions, current))
	case "subscription.resolve_avatars":
		return event.Result(handler.ResolveAvatarDataURLs(ctx, actions, payload))
	case "subscription.resolve_user":
		var input struct {
			Platform string `json:"platform"`
			Query    string `json:"query"`
		}
		if err := decodeManagementPayload(payload, &input); err != nil {
			return event.Fail("platform.invalid_request", err.Error())
		}
		return event.Result(handler.ResolveUser(ctx, actions, input.Platform, strings.TrimSpace(input.Query), current))
	case "account.list":
		result, err := handler.accountList(ctx, actions, payload)
		return accountActionResult(event, result, err)
	case "account.upsert":
		result, err := handler.accountUpsert(ctx, actions, payload)
		return accountActionResult(event, result, err)
	case "account.delete":
		result, err := handler.accountDelete(ctx, actions, payload)
		return accountActionResult(event, result, err)
	case "account.validate":
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
		result, err := handler.accountValidate(ctx, actions, payload)
		return accountActionResult(event, result, err)
	case "account.qr_create":
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
		result, err := handler.accountQRCreate(ctx, actions, payload, current)
		return accountActionResult(event, result, err)
	case "account.qr_poll":
		result, err := handler.accountQRPoll(ctx, actions, payload)
		return accountActionResult(event, result, err)
	case "account.qr_cancel":
		result, err := handler.accountQRCancel(ctx, actions, payload)
		return accountActionResult(event, result, err)
	}
	return event.Result(map[string]any{"handled": false, "message": "未知订阅与解析管理动作。"})
}

func accountActionResult(event *rayleabot.EventContext, result map[string]any, err error) error {
	if err != nil {
		return event.Fail("account.failed", DiagnosticExcerpt(err.Error(), 300))
	}
	return event.Result(result)
}

func (handler *Handler) FormatStatus(current Settings) string {
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
	names := make([]string, 0, len(handler.platforms))
	for _, platform := range handler.platforms {
		names = append(names, platform.Name)
	}
	return fmt.Sprintf("订阅与解析\n订阅状态：%s\n订阅：%d/%d\n解析：发送“解析帮助”查看当前会话开关\n平台：%s\n检查：插件定时检查，支持手动立即检查\n账号：插件“账号管理”页保存平台 Cookie", state, enabled, len(current.Subscriptions), strings.Join(names, "、"))
}
