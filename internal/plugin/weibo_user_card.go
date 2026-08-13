package plugin

import (
	"context"
	"strings"
)

const weiboUserCardTemplate = "weibo-user-card"

// buildWeiboUserCardData 生成订阅/取消订阅微博博主资料卡片的渲染输入。
// action 取值：subscribed（新订阅）、updated（更新订阅）、unsubscribed（取消订阅）。
func buildWeiboUserCardData(action string, item subscription, user weiboUser, cardServices []string) map[string]any {
	name := firstText(user.Name, item.Name, item.UID)
	uid := firstText(user.UID, item.UID)
	return map[string]any{
		"action":         action,
		"subtitle":       "微博 · 订阅中心",
		"platform":       platformName(item.Platform),
		"user":           map[string]any{"name": name, "uid": uid, "avatar": user.AvatarURL, "sign": truncateRunes(user.Sign, 80)},
		"uid_text":       uidText(uid),
		"fans_text":      user.FansText,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  servicesText(cardServices, item.Platform),
		"service_labels": serviceLabels(cardServices, item.Platform),
		"target_text":    userCardTargetText(action, item),
	}
}

// inlineWeiboCardAvatar 把可抓取的微博头像内联为 dataURL；失败返回空，模板回退到内置默认头像。
func inlineWeiboCardAvatar(ctx context.Context, actions pluginActions, sourceURL string) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return ""
	}
	if _, _, err := validateAvatarSourceURL(sourceURL); err != nil {
		return ""
	}
	dataURL, _, err := resolveAvatarDataURLWithTimeout(ctx, actions, sourceURL, weiboSearchAvatarTimeoutSeconds)
	if err != nil {
		return ""
	}
	return dataURL
}
