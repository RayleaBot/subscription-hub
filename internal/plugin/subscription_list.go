package plugin

import (
	"fmt"
	"sort"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (handler *Handler) FormatSubscriptions(current Settings, event *rayleabot.EventContext, platform string, all bool) string {
	items := make([]Subscription, 0)
	for _, item := range current.Subscriptions {
		if platform != "" && item.Platform != platform {
			continue
		}
		if !all && (item.TargetType != NormalizedTargetType(event.Event.Target.Type) || item.TargetID != event.Event.Target.ID) {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return handler.SubscriptionListTitle(platform, all) + "\n当前没有订阅。"
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	lines := []string{handler.SubscriptionListTitle(platform, all)}
	for _, item := range items {
		targetLabel := "群聊"
		if item.TargetType == "private" {
			targetLabel = "私聊"
		}
		lines = append(lines, fmt.Sprintf("%s %s · %s %s · %s · 订阅人：%s", targetLabel, item.TargetID,
			handler.platformName(item.Platform), handler.subscriptionSubjectText(item), handler.byID[item.Platform].Services.Text(item.Services), subscribersText(item)))
	}
	return strings.Join(lines, "\n")
}

func (handler *Handler) SubscriptionListTitle(platform string, all bool) string {
	if platform == "" {
		if all {
			return "全部订阅列表"
		}
		return "订阅列表"
	}
	definition := handler.byID[platform]
	if all {
		return definition.AllListTitle
	}
	return definition.ListTitle
}

func (handler *Handler) subscriptionSubjectText(item Subscription) string {
	name, uid := strings.TrimSpace(item.Name), strings.TrimSpace(item.UID)
	if name == "" || name == uid {
		return FirstText(uid, name)
	}
	label := handler.byID[item.Platform].SubjectLabel
	return fmt.Sprintf("%s（%s %s）", name, label, uid)
}
