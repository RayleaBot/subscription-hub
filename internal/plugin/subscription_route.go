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
		if !item.NeedsBotSelection && !validSubscriptionRoute(item.SourceProtocol, item.BotID) {
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
		if !target.NeedsBotSelection && !validSubscriptionRoute(target.SourceProtocol, target.BotID) {
			return fmt.Errorf("解析目标 %s 请选择机器人", target.TargetID)
		}
	}
	return nil
}

// Loading never rejects a whole configuration because one legacy route is
// unresolved. Only the old adapter's confirmed identity can repair that route.
func migrateSettingsRoutes(current *Settings, bots []rayleabot.Bot) bool {
	changed := false
	repair := func(protocol string, adapter, botID *string) bool {
		if *botID == "" && *adapter != "" {
			for _, bot := range bots {
				if bot.SourceProtocol == protocol && bot.SourceAdapter == *adapter && bot.ID != "" {
					*botID = bot.ID
					break
				}
			}
		}
		// The adapter hint stays on unresolved routes so a later identity snapshot can still repair them.
		if *botID != "" && *adapter != "" {
			*adapter = ""
			changed = true
		}
		return !validSubscriptionRoute(protocol, *botID)
	}
	for i := range current.Subscriptions {
		item := &current.Subscriptions[i]
		item.NeedsBotSelection = repair(item.SourceProtocol, &item.SourceAdapter, &item.BotID)
	}
	for i := range current.Resolver.Targets {
		target := &current.Resolver.Targets[i]
		target.NeedsBotSelection = repair(target.SourceProtocol, &target.SourceAdapter, &target.BotID)
	}
	return changed
}

func subscriptionIdentity(item Subscription) string {
	return strings.Join([]string{item.Platform, item.UID, item.SourceProtocol, item.BotID, item.TargetType, item.TargetID}, "\x00")
}
