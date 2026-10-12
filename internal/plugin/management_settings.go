package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func managementBot(event *rayleabot.EventContext) (rayleabot.BotAccount, error) {
	bot, ok := event.Event.ManagementBot()
	if !ok {
		return bot, fmt.Errorf("请选择机器人账号")
	}
	if bot.SourceProtocol != "onebot11" {
		return bot, fmt.Errorf("当前功能仅支持 OneBot11")
	}
	return bot, nil
}

func belongsToBot(protocol, id string, bot rayleabot.BotAccount) bool {
	return protocol == bot.SourceProtocol && id == bot.BotID
}

func settingsForBot(current Settings, bot rayleabot.BotAccount) Settings {
	subscriptions := make([]Subscription, 0)
	for _, item := range current.Subscriptions {
		if belongsToBot(item.SourceProtocol, item.BotID, bot) {
			subscriptions = append(subscriptions, item)
		}
	}
	targets := make([]ResolverTarget, 0)
	for _, item := range current.Resolver.Targets {
		if belongsToBot(item.SourceProtocol, item.BotID, bot) {
			targets = append(targets, item)
		}
	}
	current.Subscriptions, current.Resolver.Targets = subscriptions, targets
	return current
}

func decodeManagementPayload(payload map[string]any, result any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	return decoder.Decode(result)
}

func validateSubmittedBot(protocol, id string, bot rayleabot.BotAccount) error {
	if (protocol != "" && protocol != bot.SourceProtocol) || (id != "" && id != bot.BotID) {
		return fmt.Errorf("记录不属于当前机器人账号")
	}
	return nil
}

// All account writes replace only this account's collection, inside the same
// lock used by chat commands and global strategy saves.
func (handler *Handler) saveManagementSettings(ctx context.Context, event *rayleabot.EventContext, action string, payload map[string]any) (map[string]any, error) {
	var bot rayleabot.BotAccount
	var err error
	if action != "strategy.save" {
		bot, err = managementBot(event)
	}
	if err != nil {
		return nil, err
	}
	handler.settingsMu.Lock()
	defer handler.settingsMu.Unlock()
	current, err := handler.loadSettingsLocked(ctx, event)
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	switch action {
	case "subscription.save":
		var input struct {
			Subscriptions *[]Subscription `json:"subscriptions"`
		}
		if err := decodeManagementPayload(payload, &input); err != nil {
			return nil, err
		}
		if input.Subscriptions == nil {
			return nil, fmt.Errorf("缺少订阅列表")
		}
		owners := map[string]Subscription{}
		remaining := make([]Subscription, 0)
		for _, item := range current.Subscriptions {
			owners[item.ID] = item
			if !belongsToBot(item.SourceProtocol, item.BotID, bot) {
				remaining = append(remaining, item)
			}
		}
		seen := map[string]bool{}
		for _, item := range *input.Subscriptions {
			if err := validateSubmittedBot(item.SourceProtocol, item.BotID, bot); err != nil {
				return nil, err
			}
			item.SourceProtocol, item.BotID = bot.SourceProtocol, bot.BotID
			if handler.normalizePlatform(item.Platform) == "" || strings.TrimSpace(item.UID) == "" || strings.TrimSpace(item.TargetID) == "" || (item.TargetType != "group" && item.TargetType != "private") {
				return nil, fmt.Errorf("订阅对象或推送目标无效")
			}
			if item.ID == "" {
				item.ID = SubscriptionID(item.Platform, item.UID, bot.SourceProtocol, bot.BotID, item.TargetType, item.TargetID)
			}
			if previous, exists := owners[item.ID]; exists && !belongsToBot(previous.SourceProtocol, previous.BotID, bot) {
				return nil, fmt.Errorf("订阅不属于当前机器人账号")
			}
			if seen[item.ID] {
				return nil, fmt.Errorf("订阅 ID 重复")
			}
			seen[item.ID] = true
			remaining = append(remaining, item)
		}
		current.Subscriptions = handler.NormalizeSubscriptions(remaining)
		values["subscriptions"] = current.Subscriptions
	case "resolver.save":
		var input struct {
			Targets *[]ResolverTarget `json:"targets"`
		}
		if err := decodeManagementPayload(payload, &input); err != nil {
			return nil, err
		}
		if input.Targets == nil {
			return nil, fmt.Errorf("缺少解析目标列表")
		}
		remaining := make([]ResolverTarget, 0)
		for _, target := range current.Resolver.Targets {
			if !belongsToBot(target.SourceProtocol, target.BotID, bot) {
				remaining = append(remaining, target)
			}
		}
		for _, target := range *input.Targets {
			if err := validateSubmittedBot(target.SourceProtocol, target.BotID, bot); err != nil {
				return nil, err
			}
			if strings.TrimSpace(target.TargetID) == "" || (target.TargetType != "group" && target.TargetType != "private") {
				return nil, fmt.Errorf("解析目标无效")
			}
			target.SourceProtocol, target.BotID = bot.SourceProtocol, bot.BotID
			remaining = append(remaining, target)
		}
		current.Resolver.Targets = normalizeResolverTargets(remaining)
		values["resolver"] = current.Resolver
	case "strategy.save":
		var input struct {
			Enabled               bool                     `json:"enabled"`
			DeliveryMaxAgeMinutes int                      `json:"delivery_max_age_minutes"`
			SuperAdminWhitelist   bool                     `json:"super_admin_whitelist"`
			Cooldowns             ResolverCooldownSettings `json:"cooldowns"`
			Media                 ResolverMediaSettings    `json:"media"`
		}
		if err := decodeManagementPayload(payload, &input); err != nil {
			return nil, err
		}
		current.Enabled, current.DeliveryMaxAgeMinutes = input.Enabled, NormalizeDeliveryMaxAgeMinutes(input.DeliveryMaxAgeMinutes)
		current.Resolver.SuperAdminWhitelist, current.Resolver.Cooldowns, current.Resolver.Media = input.SuperAdminWhitelist, input.Cooldowns, input.Media
		current.Resolver = NormalizeResolverSettings(current.Resolver)
		values["enabled"], values["delivery_max_age_minutes"], values["resolver"] = current.Enabled, current.DeliveryMaxAgeMinutes, current.Resolver
	}
	if err := handler.writeSettingsLocked(ctx, event, values); err != nil {
		return nil, err
	}
	if action != "strategy.save" {
		current = settingsForBot(current, bot)
	} else {
		current.Subscriptions, current.Resolver.Targets = []Subscription{}, []ResolverTarget{}
	}
	return map[string]any{"settings": current}, nil
}
