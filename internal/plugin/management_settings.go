package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
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

func globalManagement(event *rayleabot.EventContext) error {
	if _, ok := event.Event.ManagementBot(); ok {
		return fmt.Errorf("请在全局页面操作")
	}
	return nil
}

func belongsToBot(protocol, id string, bot rayleabot.BotAccount) bool {
	return protocol == bot.SourceProtocol && id == bot.BotID
}

func settingsForBot(current Settings, bot rayleabot.BotAccount) Settings {
	subscriptions := make([]Subscription, 0)
	for _, item := range current.Subscriptions {
		if belongsToBot(item.SourceProtocol, item.BotID, bot) && !item.NeedsBotSelection {
			subscriptions = append(subscriptions, item)
		}
	}
	targets := make([]ResolverTarget, 0)
	for _, item := range current.Resolver.Targets {
		if belongsToBot(item.SourceProtocol, item.BotID, bot) && !item.NeedsBotSelection {
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

func validateSubmittedBot(protocol, id string, pending bool, bot rayleabot.BotAccount) error {
	if (protocol != "" && protocol != bot.SourceProtocol) || (id != "" && id != bot.BotID) || pending {
		return fmt.Errorf("记录不属于当前机器人账号")
	}
	return nil
}

// All account writes replace only this account's collection, inside the same
// lock used by chat commands, global strategy saves and legacy migration.
func (handler *Handler) saveManagementSettings(ctx context.Context, event *rayleabot.EventContext, action string, payload map[string]any) (map[string]any, error) {
	var bot rayleabot.BotAccount
	var err error
	if action == "strategy.save" {
		err = globalManagement(event)
	} else {
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
			if err := validateSubmittedBot(item.SourceProtocol, item.BotID, item.NeedsBotSelection, bot); err != nil {
				return nil, err
			}
			item.SourceProtocol, item.BotID, item.SourceAdapter = bot.SourceProtocol, bot.BotID, ""
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
			if err := validateSubmittedBot(target.SourceProtocol, target.BotID, target.NeedsBotSelection, bot); err != nil {
				return nil, err
			}
			if strings.TrimSpace(target.TargetID) == "" || (target.TargetType != "group" && target.TargetType != "private") {
				return nil, fmt.Errorf("解析目标无效")
			}
			target.SourceProtocol, target.BotID, target.SourceAdapter = bot.SourceProtocol, bot.BotID, ""
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

type historyRecord struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	SourceProtocol string `json:"source_protocol"`
	BotID          string `json:"bot_id"`
	SourceAdapter  string `json:"source_adapter,omitempty"`
	Label          string `json:"label"`
	TargetType     string `json:"target_type"`
	TargetID       string `json:"target_id"`
	Record         any    `json:"record"`
}

func resolverHistoryID(target ResolverTarget) string {
	encoded, _ := json.Marshal([]string{target.SourceProtocol, target.BotID, target.SourceAdapter, target.TargetType, target.TargetID})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func historyRecords(current Settings) map[string]any {
	items := make([]historyRecord, 0, len(current.Subscriptions)+len(current.Resolver.Targets))
	for _, item := range current.Subscriptions {
		items = append(items, historyRecord{"subscription", item.ID, item.SourceProtocol, item.BotID, item.SourceAdapter, item.Name, item.TargetType, item.TargetID, item})
	}
	for _, item := range current.Resolver.Targets {
		items = append(items, historyRecord{"resolver", resolverHistoryID(item), item.SourceProtocol, item.BotID, item.SourceAdapter, item.TargetName, item.TargetType, item.TargetID, item})
	}
	return map[string]any{"items": items, "total": len(items)}
}

func (handler *Handler) manageHistory(ctx context.Context, event *rayleabot.EventContext, action string, payload map[string]any) (map[string]any, error) {
	if err := globalManagement(event); err != nil {
		return nil, err
	}
	handler.settingsMu.Lock()
	defer handler.settingsMu.Unlock()
	current, err := handler.loadSettingsLocked(ctx, event)
	if err != nil {
		return nil, err
	}
	if action == "history.list" {
		return historyRecords(current), nil
	}
	var input struct {
		Kind string               `json:"kind"`
		ID   string               `json:"id"`
		Bot  rayleabot.BotAccount `json:"bot"`
	}
	if err := decodeManagementPayload(payload, &input); err != nil {
		return nil, err
	}
	assign := action == "history.assign"
	validateAssignment := func(protocol, botID string) error {
		if !assign {
			return nil
		}
		if protocol != "onebot11" || botID != "" || input.Bot.SourceProtocol != "onebot11" || input.Bot.BotID == "" {
			return fmt.Errorf("仅缺少账号的 OneBot11 记录可归属；完整账号记录不可改挂")
		}
		return nil
	}
	values := map[string]any{}
	found := false
	switch input.Kind {
	case "subscription":
		for i, item := range current.Subscriptions {
			if item.ID != input.ID {
				continue
			}
			if err := validateAssignment(item.SourceProtocol, item.BotID); err != nil {
				return nil, err
			}
			if assign {
				item.BotID, item.SourceAdapter, item.NeedsBotSelection = input.Bot.BotID, "", false
				current.Subscriptions[i] = item // Keep the ID and delivery history.
			} else {
				current.Subscriptions = append(current.Subscriptions[:i], current.Subscriptions[i+1:]...)
			}
			values["subscriptions"], found = current.Subscriptions, true
			break
		}
	case "resolver":
		for i, item := range current.Resolver.Targets {
			if resolverHistoryID(item) != input.ID {
				continue
			}
			if err := validateAssignment(item.SourceProtocol, item.BotID); err != nil {
				return nil, err
			}
			if assign {
				item.BotID, item.SourceAdapter, item.NeedsBotSelection = input.Bot.BotID, "", false
				current.Resolver.Targets[i] = item
				current.Resolver.Targets = normalizeResolverTargets(current.Resolver.Targets)
			} else {
				current.Resolver.Targets = append(current.Resolver.Targets[:i], current.Resolver.Targets[i+1:]...)
			}
			values["resolver"], found = current.Resolver, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("历史记录不存在，请刷新")
	}
	if err := handler.writeSettingsLocked(ctx, event, values); err != nil {
		return nil, err
	}
	return historyRecords(current), nil
}
