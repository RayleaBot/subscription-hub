package douyin

import (
	"context"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const douyinUserCardTemplate = "douyin-user-card"

func buildDouyinUserCardData(action string, item plugin.Subscription, user douyinUser, cardServices []string) map[string]any {
	name := plugin.FirstText(user.Name, item.Name, item.UID)
	uid := plugin.FirstText(user.UID, item.UID)
	uidTextValue := plugin.UIDText(uid)
	if uniqueID := plugin.FirstText(strings.TrimSpace(user.UniqueID), strings.TrimSpace(item.UniqueID)); uniqueID != "" {
		uidTextValue = uniqueID
	}
	return map[string]any{
		"action":         action,
		"subtitle":       "抖音 · 订阅中心",
		"platform":       "抖音",
		"user":           map[string]any{"name": name, "uid": uid, "avatar": user.AvatarURL, "sign": plugin.TruncateRunes(user.Sign, 80)},
		"uid_text":       uidTextValue,
		"fans_text":      user.FansText,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  catalog.Text(cardServices),
		"service_labels": catalog.Labels(cardServices),
		"target_text":    plugin.UserCardTargetText(action, item),
	}
}

func inlineDouyinCardAvatar(ctx context.Context, actions plugin.SourceActions, sourceURL string) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return ""
	}
	dataURL, _, err := plugin.ResolveAvatarDataURLLimited(ctx, actions, sourceURL, douyinSearchAvatarTimeoutSeconds, plugin.MaxUpdateCardAvatarBytes, avatarPolicy())
	if err != nil {
		return ""
	}
	return dataURL
}
