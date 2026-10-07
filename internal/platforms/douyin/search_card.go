package douyin

import (
	"context"
	"strings"
	"sync"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const douyinSearchResultsTemplate = "douyin-search-results"

// douyinpic 头像 CDN 实测响应 3-7 秒，搜索与资料卡片取头像需要更长超时。
const douyinSearchAvatarTimeoutSeconds = 8

const douyinSearchFallbackAvatar = "assets/douyin-default-avatar.svg"

const douyinSearchRenderErrorMessage = "抖音用户搜索结果图片生成失败，请稍后重试。"

func buildDouyinSearchCardData(query string, users []douyinUser, commandPrefixes []string) map[string]any {
	cards := make([]map[string]any, 0, len(users))
	for index, user := range users {
		uidTextValue := plugin.UIDText(user.UID)
		if uniqueID := strings.TrimSpace(user.UniqueID); uniqueID != "" {
			uidTextValue = uniqueID
		}
		cards = append(cards, map[string]any{
			"rank":        index + 1,
			"name":        plugin.FirstText(user.Name, user.UID),
			"uid_text":    uidTextValue,
			"avatar":      user.AvatarURL,
			"fans_text":   user.FansText,
			"verify_text": user.Verify,
			"verify_org":  user.VerifyOrg,
			"sign":        plugin.TruncateRunes(user.Sign, 48),
		})
	}
	return map[string]any{
		"query":       strings.TrimSpace(query),
		"subtitle":    "抖音 · 订阅中心",
		"platform":    "抖音",
		"count":       len(cards),
		"users":       cards,
		"footer_hint": douyinSubscribeHint(commandPrefixes),
	}
}

func prepareDouyinSearchAvatars(ctx context.Context, actions plugin.SourceActions, users []douyinUser) []douyinUser {
	resolved := append([]douyinUser(nil), users...)
	var wait sync.WaitGroup
	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		resolved[index].AvatarURL = douyinSearchFallbackAvatar
		if sourceURL == "" {
			continue
		}
		if _, _, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy()); err != nil {
			continue
		}
		wait.Add(1)
		go func(index int, sourceURL string) {
			defer wait.Done()
			if dataURL, _, err := plugin.ResolveAvatarDataURLWithTimeout(ctx, actions, sourceURL, douyinSearchAvatarTimeoutSeconds, avatarPolicy()); err == nil {
				resolved[index].AvatarURL = dataURL
			}
		}(index, sourceURL)
	}
	wait.Wait()
	return resolved
}

func douyinSubscribeHint(commandPrefixes []string) string {
	for _, value := range commandPrefixes {
		prefix := strings.TrimSpace(value)
		if prefix != "" {
			return "使用前缀 " + prefix + "：" + prefix + "订阅抖音推送 [类型] 用户主页链接或完整 sec_uid"
		}
	}
	return "使用“前缀 + 订阅抖音推送 [类型] 用户主页链接或完整 sec_uid”"
}
