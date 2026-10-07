package plugin

import (
	"context"
	"encoding/json"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/assets"
)

const defaultDeliveryMaxAgeMinutes = 30
const minimumDeliveryMaxAgeMinutes = 1
const maximumDeliveryMaxAgeMinutes = 24 * 60

const defaultAccountCheckIntervalMinutes = 360
const minimumAccountCheckIntervalMinutes = 15
const maximumAccountCheckIntervalMinutes = 10080

func (handler *Handler) loadSettings(ctx context.Context, event *rayleabot.EventContext) (Settings, error) {
	current := Settings{Enabled: true, DeliveryMaxAgeMinutes: defaultDeliveryMaxAgeMinutes, AccountCheckIntervalMinutes: defaultAccountCheckIntervalMinutes, Subscriptions: []Subscription{}}
	_ = json.Unmarshal(assets.DefaultConfigJSON, &current)
	values := event.Config
	if enabled, ok := values["enabled"].(bool); ok {
		current.Enabled = enabled
	}
	if raw, ok := values["delivery_max_age_minutes"]; ok {
		current.DeliveryMaxAgeMinutes = int(IntScalar(raw))
	}
	if raw, ok := values["account_check_interval_minutes"]; ok {
		current.AccountCheckIntervalMinutes = int(IntScalar(raw))
	}
	if raw, ok := values["account_browser_mode"]; ok {
		current.AccountBrowserMode = strings.TrimSpace(StringScalar(raw))
	}
	if raw, ok := values["account_browser_remote_debugging_url"]; ok {
		current.AccountBrowserRemoteURL = strings.TrimSpace(StringScalar(raw))
	}
	if raw, ok := values["subscriptions"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &current.Subscriptions)
	}
	if raw, ok := values["resolver"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &current.Resolver)
	}
	current.DeliveryMaxAgeMinutes = NormalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes)
	current.AccountCheckIntervalMinutes = NormalizeAccountCheckIntervalMinutes(current.AccountCheckIntervalMinutes)
	current.Subscriptions = handler.NormalizeSubscriptions(current.Subscriptions)
	current.Resolver = NormalizeResolverSettings(current.Resolver)
	NormalizeSubscriptionSubscribers(current.Subscriptions, event.SuperAdmins)
	handler.ensureScheduler(ctx, event)
	return current, nil
}

func (handler *Handler) saveSettings(ctx context.Context, event *rayleabot.EventContext, current Settings) error {
	_, err := handler.hostActions(event).ConfigWrite(ctx, map[string]any{
		"enabled":                              current.Enabled,
		"delivery_max_age_minutes":             NormalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes),
		"account_check_interval_minutes":       NormalizeAccountCheckIntervalMinutes(current.AccountCheckIntervalMinutes),
		"account_browser_mode":                 NormalizeAccountBrowserMode(current.AccountBrowserMode),
		"account_browser_remote_debugging_url": strings.TrimSpace(current.AccountBrowserRemoteURL),
		"subscriptions":                        current.Subscriptions,
		"resolver":                             NormalizeResolverSettings(current.Resolver),
	})
	return err
}

func NormalizeAccountBrowserMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "visible", "headless", "remote_cdp":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "auto"
	}
}

func NormalizeAccountCheckIntervalMinutes(value int) int {
	if value <= 0 {
		return 0
	}
	if value < minimumAccountCheckIntervalMinutes {
		return minimumAccountCheckIntervalMinutes
	}
	if value > maximumAccountCheckIntervalMinutes {
		return maximumAccountCheckIntervalMinutes
	}
	return value
}

func NormalizeDeliveryMaxAgeMinutes(value int) int {
	if value < minimumDeliveryMaxAgeMinutes {
		return defaultDeliveryMaxAgeMinutes
	}
	if value > maximumDeliveryMaxAgeMinutes {
		return maximumDeliveryMaxAgeMinutes
	}
	return value
}

func (handler *Handler) NormalizeSubscriptions(items []Subscription) []Subscription {
	seen := map[string]bool{}
	result := make([]Subscription, 0, len(items))
	for _, item := range items {
		item.Platform = handler.normalizePlatform(item.Platform)
		item.UID = strings.TrimSpace(item.UID)
		item.TargetType = NormalizedTargetType(item.TargetType)
		item.TargetID = strings.TrimSpace(item.TargetID)
		if item.Platform == "" || item.UID == "" || item.TargetID == "" {
			continue
		}
		if item.ID == "" {
			item.ID = SubscriptionID(item.Platform, item.UID, item.TargetType, item.TargetID)
		}
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		if item.Name == "" {
			item.Name = item.UID
		}
		item.Services = handler.byID[item.Platform].Services.NormalizeAll(item.Services)
		result = append(result, item)
	}
	return result
}
