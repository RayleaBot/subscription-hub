package plugin

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
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
	handler.settingsMu.Lock()
	defer handler.settingsMu.Unlock()
	return handler.loadSettingsLocked(ctx, event)
}

func (handler *Handler) loadSettingsLocked(ctx context.Context, event *rayleabot.EventContext) (Settings, error) {
	current := Settings{Enabled: true, DeliveryMaxAgeMinutes: defaultDeliveryMaxAgeMinutes, AccountCheckIntervalMinutes: defaultAccountCheckIntervalMinutes, Subscriptions: []Subscription{}}
	_ = json.Unmarshal(assets.DefaultConfigJSON, &current)
	values := map[string]any{}
	maps.Copy(values, event.Config)
	maps.Copy(values, handler.settingsOverrides)
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
		if err := json.Unmarshal(encoded, &current.Subscriptions); err != nil {
			return Settings{}, err
		}
	}
	if raw, ok := values["resolver"]; ok {
		encoded, _ := json.Marshal(raw)
		if err := json.Unmarshal(encoded, &current.Resolver); err != nil {
			return Settings{}, err
		}
	}
	if migrateSettingsRoutes(&current, event.Bots) {
		if err := handler.writeSettingsLocked(ctx, event, map[string]any{"subscriptions": current.Subscriptions, "resolver": current.Resolver}); err != nil {
			_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
				Level: "warn", Message: "机器人账号迁移保存失败，旧配置已保留，下次加载重试：" + err.Error(),
			})
		}
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
	handler.settingsMu.Lock()
	defer handler.settingsMu.Unlock()
	return handler.saveSettingsLocked(ctx, event, current)
}

func (handler *Handler) saveSettingsLocked(ctx context.Context, event *rayleabot.EventContext, current Settings) error {
	if err := validateSettingsRoutes(current); err != nil {
		return err
	}
	return handler.writeSettingsLocked(ctx, event, map[string]any{
		"subscriptions": current.Subscriptions,
		"resolver":      NormalizeResolverSettings(current.Resolver),
	})
}

// Event.Config is captured before a handler acquires our lock. Keep successful
// writes here so queued account saves and chat commands merge into the latest
// state. JSON decoding also separates mutable settings from this snapshot.
func (handler *Handler) writeSettingsLocked(ctx context.Context, event *rayleabot.EventContext, values map[string]any) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var snapshot map[string]any
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return err
	}
	result, err := handler.hostActions(event).ConfigWrite(ctx, values)
	if err != nil {
		return err
	}
	if handler.settingsOverrides == nil {
		handler.settingsOverrides = map[string]any{}
	}
	if handler.settingsEchoes == nil {
		handler.settingsEchoes = map[string][]string{}
	}
	maps.Copy(handler.settingsOverrides, snapshot)
	encoded, _ = json.Marshal(result["changed_keys"])
	var changed []string
	_ = json.Unmarshal(encoded, &changed)
	for _, key := range changed {
		value, _ := json.Marshal(snapshot[key])
		handler.settingsEchoes[key] = append(handler.settingsEchoes[key], string(value))
	}
	return nil
}

func (handler *Handler) settingsChanged(ctx context.Context, event *rayleabot.EventContext) error {
	handler.settingsMu.Lock()
	defer handler.settingsMu.Unlock()
	config := MapValue(event.Event.Payload["config"])
	encoded, _ := json.Marshal(event.Event.Payload["changed_keys"])
	var changed []string
	_ = json.Unmarshal(encoded, &changed)
	for _, key := range changed {
		value, _ := json.Marshal(config[key])
		echoes := handler.settingsEchoes[key]
		if index := slices.Index(echoes, string(value)); index >= 0 {
			handler.settingsEchoes[key] = slices.Delete(echoes, index, index+1)
			continue // An older notification of our own write cannot undo a newer save.
		}
		if handler.settingsOverrides == nil {
			handler.settingsOverrides = map[string]any{}
		}
		handler.settingsOverrides[key] = config[key]
	}
	_, err := handler.loadSettingsLocked(ctx, event)
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
			item.ID = SubscriptionID(item.Platform, item.UID, item.SourceProtocol, item.BotID, item.TargetType, item.TargetID)
		}
		key := item.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		if item.Name == "" {
			item.Name = item.UID
		}
		item.Services = handler.byID[item.Platform].Services.NormalizeAll(item.Services)
		result = append(result, item)
	}
	return result
}
