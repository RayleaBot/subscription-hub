package weibo

import (
	"context"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const weiboUserCardTemplate = "weibo-user-card"

// buildWeiboUserCardData 生成订阅/取消订阅微博博主资料卡片的渲染输入。
// action 取值：subscribed（新订阅）、updated（更新订阅）、unsubscribed（取消订阅）。
func buildWeiboUserCardData(action string, item plugin.Subscription, user weiboUser, cardServices []string) map[string]any {
	name := plugin.FirstText(user.Name, item.Name, item.UID)
	uid := plugin.FirstText(user.UID, item.UID)
	return map[string]any{
		"action":         action,
		"subtitle":       "微博 · 订阅中心",
		"platform":       "微博",
		"user":           map[string]any{"name": name, "uid": uid, "avatar": user.AvatarURL, "sign": plugin.TruncateRunes(user.Sign, 80)},
		"uid_text":       plugin.UIDText(uid),
		"fans_text":      user.FansText,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  catalog.Text(cardServices),
		"service_labels": catalog.Labels(cardServices),
		"target_text":    plugin.UserCardTargetText(action, item),
	}
}

// inlineWeiboCardAvatar 把可抓取的微博头像内联为 dataURL；失败返回空，模板回退到内置默认头像。
func inlineWeiboCardAvatar(ctx context.Context, actions plugin.SourceActions, sourceURL string) string {
	sourceURL = weiboCardAvatarURL(sourceURL)
	if sourceURL == "" {
		return ""
	}
	dataURL, _, err := plugin.ResolveAvatarDataURLLimited(ctx, actions, sourceURL, weiboSearchAvatarTimeoutSeconds, plugin.MaxUpdateCardAvatarBytes, avatarPolicy())
	if err != nil {
		return ""
	}
	return dataURL
}
