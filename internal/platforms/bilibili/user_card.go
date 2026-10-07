package bilibili

import (
	"context"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const bilibiliUserCardTemplate = "bilibili-user-card"

// buildBilibiliUserCardData 生成订阅/取消订阅 UP 主资料卡片的渲染输入。
// action 取值：subscribed（新订阅）、updated（更新订阅）、unsubscribed（取消订阅）。
func buildBilibiliUserCardData(action string, item plugin.Subscription, user bilibiliUser, cardServices []string) map[string]any {
	name := plugin.FirstText(user.Name, item.Name, item.UID)
	uid := plugin.FirstText(user.UID, item.UID)
	levelIcon := ""
	if user.Level > 0 || user.Senior {
		levelIcon = bilibiliLevelIcon(user.Level, user.Senior)
	}
	return map[string]any{
		"action":         action,
		"subtitle":       "Bilibili · 订阅中心",
		"platform":       "Bilibili",
		"user":           map[string]any{"name": name, "uid": uid, "avatar": plugin.FirstText(user.AvatarURL, item.AvatarURL), "fans": user.Fans, "sign": plugin.TruncateRunes(user.Sign, 80)},
		"uid_text":       plugin.UIDText(uid),
		"fans_text":      plugin.FansText(user.Fans),
		"videos_text":    videosText(user.Videos),
		"level_icon":     levelIcon,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  catalog.Text(cardServices),
		"service_labels": catalog.Labels(cardServices),
		"target_text":    plugin.UserCardTargetText(action, item),
	}
}

// inlineBilibiliCardAvatar 把资料卡片头像内联为 dataURL，避免渲染时远程图片与截图竞态
// （远程加载慢时截图抓到破图，卡住时渲染超时降级为文字）。失败返回空，模板回退默认头像。
func inlineBilibiliCardAvatar(ctx context.Context, actions plugin.SourceActions, sourceURL string) string {
	return inlineBilibiliCardAvatarWithTimeout(ctx, actions, sourceURL, bilibiliSearchAvatarTimeoutSeconds)
}

func inlineBilibiliCardAvatarWithTimeout(ctx context.Context, actions plugin.SourceActions, sourceURL string, timeoutSeconds int) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" || isBilibiliDefaultAvatarURL(sourceURL) {
		return ""
	}
	requestURL, trusted := bilibiliSearchAvatarURL(sourceURL)
	if !trusted {
		return ""
	}
	seen := make(map[string]struct{}, 2)
	for _, candidate := range []string{requestURL, sourceURL} {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		if dataURL, _, err := plugin.ResolveAvatarDataURLLimited(ctx, actions, candidate, timeoutSeconds, plugin.MaxUpdateCardAvatarBytes, avatarPolicy()); err == nil {
			return dataURL
		}
	}
	return ""
}
