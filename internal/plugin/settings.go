package plugin

import (
	"context"
	"encoding/json"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/assets"
)

const defaultDeliveryMaxAgeMinutes = 30
const minimumDeliveryMaxAgeMinutes = 1
const maximumDeliveryMaxAgeMinutes = 24 * 60

func (handler *Handler) loadSettings(ctx context.Context, event *rayleabot.EventContext) (Settings, error) {
	current := Settings{Enabled: true, DeliveryMaxAgeMinutes: defaultDeliveryMaxAgeMinutes, Subscriptions: []Subscription{}}
	_ = json.Unmarshal(assets.DefaultConfigJSON, &current)
	result, err := handler.hostActions(event).ConfigRead(ctx, "enabled", "delivery_max_age_minutes", "subscriptions")
	if err != nil {
		_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
			Level: "warn", Message: "订阅设置读取失败，使用默认设置", Fields: map[string]any{"error": err.Error()},
		})
		handler.ensureScheduler(ctx, event)
		return current, nil
	}
	values, _ := result["values"].(map[string]any)
	if enabled, ok := values["enabled"].(bool); ok {
		current.Enabled = enabled
	}
	if raw, ok := values["delivery_max_age_minutes"]; ok {
		current.DeliveryMaxAgeMinutes = int(IntScalar(raw))
	}
	if raw, ok := values["subscriptions"]; ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &current.Subscriptions)
	}
	current.DeliveryMaxAgeMinutes = NormalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes)
	current.Subscriptions = handler.NormalizeSubscriptions(current.Subscriptions)
	NormalizeSubscriptionSubscribers(current.Subscriptions, event.SuperAdmins)
	handler.ensureScheduler(ctx, event)
	return current, nil
}

func (handler *Handler) saveSettings(ctx context.Context, event *rayleabot.EventContext, current Settings) error {
	_, err := handler.hostActions(event).ConfigWrite(ctx, map[string]any{
		"enabled":                  current.Enabled,
		"delivery_max_age_minutes": NormalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes),
		"subscriptions":            current.Subscriptions,
	})
	return err
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
