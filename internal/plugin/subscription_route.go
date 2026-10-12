package plugin

import (
	"fmt"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func validSubscriptionRoute(protocol, botID string) bool {
	return protocol == "onebot11" && strings.TrimSpace(botID) != ""
}

func subscriptionTargetsEvent(item Subscription, event *rayleabot.EventContext) bool {
	return validSubscriptionRoute(item.SourceProtocol, item.BotID) && item.SourceProtocol == event.Event.SourceProtocol && item.BotID == event.Event.BotID &&
		item.TargetType == NormalizedTargetType(event.Event.Target.Type) && item.TargetID == event.Event.Target.ID
}

func validateSettingsRoutes(current Settings) error {
	ids := make(map[string]bool, len(current.Subscriptions))
	for _, item := range current.Subscriptions {
		if !validSubscriptionRoute(item.SourceProtocol, item.BotID) {
			return fmt.Errorf("订阅 %s 请选择机器人", item.ID)
		}
		if item.ID != "" {
			if ids[item.ID] {
				return fmt.Errorf("订阅 ID %s 重复", item.ID)
			}
			ids[item.ID] = true
		}
	}
	for _, target := range current.Resolver.Targets {
		if !validSubscriptionRoute(target.SourceProtocol, target.BotID) {
			return fmt.Errorf("解析目标 %s 请选择机器人", target.TargetID)
		}
	}
	return nil
}

func subscriptionIdentity(item Subscription) string {
	return strings.Join([]string{item.Platform, item.UID, item.SourceProtocol, item.BotID, item.TargetType, item.TargetID}, "\x00")
}
