package plugin

import (
	"context"
	"strings"
)

const douyinUserCardTemplate = "douyin-user-card"

func buildDouyinUserCardData(action string, item subscription, user douyinUser, cardServices []string) map[string]any {
	name := firstText(user.Name, item.Name, item.UID)
	uid := firstText(user.UID, item.UID)
	uidTextValue := uidText(uid)
	if uniqueID := firstText(strings.TrimSpace(user.UniqueID), strings.TrimSpace(item.UniqueID)); uniqueID != "" {
		uidTextValue = uniqueID
	}
	return map[string]any{
		"action":         action,
		"subtitle":       "抖音 · 订阅中心",
		"platform":       platformName(item.Platform),
		"user":           map[string]any{"name": name, "uid": uid, "avatar": user.AvatarURL, "sign": truncateRunes(user.Sign, 80)},
		"uid_text":       uidTextValue,
		"fans_text":      user.FansText,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  servicesText(cardServices, item.Platform),
		"service_labels": serviceLabels(cardServices, item.Platform),
		"target_text":    userCardTargetText(action, item),
	}
}

func inlineDouyinCardAvatar(ctx context.Context, actions pluginActions, sourceURL string) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return ""
	}
	dataURL, _, err := resolveAvatarDataURLLimited(ctx, actions, sourceURL, weiboSearchAvatarTimeoutSeconds, maxUpdateCardAvatarBytes)
	if err != nil {
		return ""
	}
	return dataURL
}
