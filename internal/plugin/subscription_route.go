package plugin

import (
	"fmt"
	"regexp"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

var subscriptionAdapterPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validSubscriptionRoute(protocol, adapter string) bool {
	return protocol == "onebot11" && subscriptionAdapterPattern.MatchString(adapter)
}

func subscriptionTargetsEvent(item Subscription, event *rayleabot.EventContext) bool {
	return item.SourceProtocol == event.Event.SourceProtocol && item.SourceAdapter == event.Event.SourceAdapter &&
		item.TargetType == NormalizedTargetType(event.Event.Target.Type) && item.TargetID == event.Event.Target.ID
}

func validateSettingsRoutes(current Settings) error {
	ids := make(map[string]bool, len(current.Subscriptions))
	for _, item := range current.Subscriptions {
		if !validSubscriptionRoute(item.SourceProtocol, item.SourceAdapter) {
			return fmt.Errorf("订阅 %s 缺少有效的 OneBot11 发送连接", item.ID)
		}
		if item.ID != "" {
			if ids[item.ID] {
				return fmt.Errorf("订阅 ID %s 重复", item.ID)
			}
			ids[item.ID] = true
		}
	}
	for _, target := range current.Resolver.Targets {
		if !validSubscriptionRoute(target.SourceProtocol, target.SourceAdapter) {
			return fmt.Errorf("解析目标 %s 缺少有效的 OneBot11 连接", target.TargetID)
		}
	}
	return nil
}

func subscriptionIdentity(item Subscription) string {
	return strings.Join([]string{item.Platform, item.UID, item.SourceProtocol, item.SourceAdapter, item.TargetType, item.TargetID}, "\x00")
}
