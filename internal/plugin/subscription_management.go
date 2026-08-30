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
	id := SubscriptionID(platform, user.UID, targetType, event.Event.Target.ID)
	for index := range current.Subscriptions {
		item := &current.Subscriptions[index]
		if item.ID != id {
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
		if item.Platform == platform && matches && item.TargetType == NormalizedTargetType(event.Event.Target.Type) && item.TargetID == event.Event.Target.ID {
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
	if action == "subscription.check_now" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
	}
	current, err := handler.loadSettings(ctx, event)
	if err != nil {
		return err
	}
	actions := handler.hostActions(event)
	switch action {
	case "subscription.check_now":
		return event.Result(handler.Check(ctx, actions, current))
	case "subscription.resolve_avatars":
		return event.Result(handler.ResolveAvatarDataURLs(ctx, actions, payload))
	case "subscription.resolve_user":
		platform := stringValue(payload, "platform", handler.platforms[0].ID)
		return event.Result(handler.ResolveUser(ctx, actions, platform, stringValue(payload, "query", ""), current))
	}
	return event.Result(map[string]any{"handled": false, "message": "未知订阅与解析管理动作。"})
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
	return fmt.Sprintf("订阅与解析\n订阅状态：%s\n订阅：%d/%d\n解析：发送“解析帮助”查看当前会话开关\n平台：%s\n检查：插件定时检查，支持手动立即检查\n账号：Web 三方账号页面管理平台 Cookie", state, enabled, len(current.Subscriptions), strings.Join(names, "、"))
}
