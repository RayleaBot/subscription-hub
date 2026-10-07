package weibo

import (
	"context"
	"strings"
	"sync"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const weiboSearchResultsTemplate = "weibo-search-results"

const weiboSearchAvatarTimeoutSeconds = 6

const weiboSearchFallbackAvatar = "assets/weibo-default-avatar.svg"

const weiboSearchRenderErrorMessage = "微博博主搜索结果图片生成失败，请稍后重试。"

// buildWeiboSearchCardData 生成微博博主搜索结果卡片的渲染输入。
func buildWeiboSearchCardData(query string, users []weiboUser, commandPrefixes []string) map[string]any {
	cards := make([]map[string]any, 0, len(users))
	for index, user := range users {
		cards = append(cards, map[string]any{
			"rank":        index + 1,
			"name":        plugin.FirstText(user.Name, user.UID),
			"uid_text":    plugin.UIDText(user.UID),
			"avatar":      user.AvatarURL,
			"fans_text":   user.FansText,
			"verify_text": user.Verify,
			"verify_org":  user.VerifyOrg,
			"sign":        plugin.TruncateRunes(user.Sign, 48),
		})
	}
	return map[string]any{
		"query":       strings.TrimSpace(query),
		"subtitle":    "微博 · 订阅中心",
		"platform":    "微博",
		"count":       len(cards),
		"users":       cards,
		"footer_hint": weiboSubscribeHint(commandPrefixes),
	}
}

// prepareWeiboSearchAvatars 并发解析全部搜索结果的内联头像。
// 单个头像无法读取时使用模板内置的默认头像，不阻断其余搜索结果。
func prepareWeiboSearchAvatars(ctx context.Context, actions plugin.SourceActions, users []weiboUser) []weiboUser {
	resolved := append([]weiboUser(nil), users...)
	var wait sync.WaitGroup

	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		resolved[index].AvatarURL = weiboSearchFallbackAvatar
		if sourceURL == "" {
			continue
		}
		if _, _, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy()); err != nil {
			continue
		}

		wait.Add(1)
		go func(index int, sourceURL string) {
			defer wait.Done()
			if dataURL, _, err := plugin.ResolveAvatarDataURLWithTimeout(ctx, actions, sourceURL, weiboSearchAvatarTimeoutSeconds, avatarPolicy()); err == nil {
				resolved[index].AvatarURL = dataURL
			}
		}(index, sourceURL)
	}

	wait.Wait()
	return resolved
}

func weiboSubscribeHint(commandPrefixes []string) string {
	for _, value := range commandPrefixes {
		prefix := strings.TrimSpace(value)
		if prefix != "" {
			return "使用前缀 " + prefix + "：" + prefix + "订阅微博推送 [类型] UID或昵称"
		}
	}
	return "使用“前缀 + 订阅微博推送 [类型] UID或昵称”"
}
