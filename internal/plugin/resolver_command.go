package plugin

import (
	"context"
	"fmt"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (handler *Handler) toggleResolverForCurrentTarget(ctx context.Context, event *rayleabot.EventContext, current *Settings, operation string) error {
	platform := ""
	for _, candidate := range []string{"bilibili", "weibo", "douyin"} {
		if strings.HasSuffix(operation, candidate) {
			platform = candidate
			break
		}
	}
	if platform == "" || event.Event.Target.ID == "" {
		return event.SendText("当前会话无法修改解析设置。")
	}
	enabled := strings.Contains(operation, "_enable_")
	targetType := NormalizedTargetType(event.Event.Target.Type)
	targetID := strings.TrimSpace(event.Event.Target.ID)
	index := -1
	for currentIndex := range current.Resolver.Targets {
		target := &current.Resolver.Targets[currentIndex]
		if target.TargetType == targetType && target.TargetID == targetID {
			index = currentIndex
			break
		}
	}
	if index < 0 {
		current.Resolver.Targets = append(current.Resolver.Targets, ResolverTarget{
			TargetType: targetType,
			TargetID:   targetID,
			TargetName: CurrentTargetName(event),
		})
		index = len(current.Resolver.Targets) - 1
	}
	target := &current.Resolver.Targets[index]
	if name := CurrentTargetName(event); name != "" {
		target.TargetName = name
	}
	target.SetPlatform(platform, enabled)
	current.Resolver = NormalizeResolverSettings(current.Resolver)
	if err := handler.saveSettings(ctx, event, *current); err != nil {
		return err
	}
	state := "关闭"
	if enabled {
		state = "开启"
	}
	return event.SendText(fmt.Sprintf("已为当前%s%s%s解析。", targetTypeLabel(targetType), state, resolverPlatformLabel(platform)))
}

func (handler *Handler) toggleResolverSuperAdminWhitelist(ctx context.Context, event *rayleabot.EventContext, current *Settings, enabled bool) error {
	current.Resolver.SuperAdminWhitelist = enabled
	current.Resolver = NormalizeResolverSettings(current.Resolver)
	if err := handler.saveSettings(ctx, event, *current); err != nil {
		return err
	}
	if enabled {
		return event.SendText("已开启超级管理员白名单：超级管理员发送的链接不受群聊、私聊解析开关限制，全部解析。")
	}
	return event.SendText("已关闭超级管理员白名单：超级管理员发送的链接按各会话的解析开关处理。")
}

func (handler *Handler) replyResolverHelp(ctx context.Context, event *rayleabot.EventContext, current Settings) error {
	target, _ := current.Resolver.Target(event.Event.Target.Type, event.Event.Target.ID)
	prefix := "/"
	if len(event.CommandPrefixes) > 0 && strings.TrimSpace(event.CommandPrefixes[0]) != "" {
		prefix = event.CommandPrefixes[0]
	}
	data := map[string]any{
		"target_type": targetTypeLabel(target.TargetType),
		"target_name": FirstText(target.TargetName, CurrentTargetName(event), event.Event.Target.ID),
		"platforms": []map[string]any{
			{"name": "B站", "enabled": target.Bilibili, "enable_command": prefix + "开启B站解析", "disable_command": prefix + "关闭B站解析"},
			{"name": "微博", "enabled": target.Weibo, "enable_command": prefix + "开启微博解析", "disable_command": prefix + "关闭微博解析"},
			{"name": "抖音", "enabled": target.Douyin, "enable_command": prefix + "开启抖音解析", "disable_command": prefix + "关闭抖音解析"},
		},
		"super_admin_whitelist": current.Resolver.SuperAdminWhitelist,
		"same_link_enabled":     current.Resolver.Cooldowns.SameLinkEnabled,
		"same_link_seconds":     current.Resolver.Cooldowns.SameLinkSeconds,
		"same_platform_enabled": current.Resolver.Cooldowns.SamePlatformEnabled,
		"same_platform_seconds": current.Resolver.Cooldowns.SamePlatformSeconds,
		"live_record_seconds":   current.Resolver.Media.LiveRecordSeconds,
		"image_batch_size":      current.Resolver.Media.ImageBatchSize,
	}
	card := CardRequest{Template: "resolver-help", Data: data, Fallback: resolverHelpText(target, current.Resolver, prefix)}
	return handler.replyCard(ctx, event, "resolver", card)
}

func resolverHelpText(target ResolverTarget, settings ResolverSettings, prefix string) string {
	state := func(value bool) string {
		if value {
			return "开启"
		}
		return "关闭"
	}
	return fmt.Sprintf("订阅与解析\nB站：%s\n微博：%s\n抖音：%s\n超管白名单：%s\n同链接冷却：%s（%d秒）\n同平台冷却：%s（%d秒）\n管理指令：%s开启/关闭XX解析、%s开启/关闭超管解析",
		state(target.Bilibili), state(target.Weibo), state(target.Douyin), state(settings.SuperAdminWhitelist), state(settings.Cooldowns.SameLinkEnabled), settings.Cooldowns.SameLinkSeconds,
		state(settings.Cooldowns.SamePlatformEnabled), settings.Cooldowns.SamePlatformSeconds, prefix, prefix)
}

func targetTypeLabel(value string) string {
	if NormalizedTargetType(value) == "private" {
		return "私聊"
	}
	return "群聊"
}

func resolverPlatformLabel(value string) string {
	switch value {
	case "bilibili":
		return "B站"
	case "weibo":
		return "微博"
	case "douyin":
		return "抖音"
	default:
		return value
	}
}
