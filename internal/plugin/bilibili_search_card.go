package plugin

import (
	"context"
	"strconv"
	"strings"
	"sync"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliSearchResultsTemplate      = "bilibili-search-results"
	maxBilibiliSearchAvatarConcurrency = 10
)

// replyBilibiliUserSearch 搜索 UP 主并回复结果卡片；渲染失败时降级为文字列表。
func replyBilibiliUserSearch(ctx context.Context, event *rayleabot.EventContext, query string) error {
	users, err := searchBilibili(ctx, event, query)
	if err != nil {
		return event.SendText(friendlyBilibiliError(err))
	}
	users = inlineBilibiliSearchAvatars(ctx, event.Actions(), users)
	data := buildBilibiliSearchCardData(query, users, event.CommandPrefixes)
	return sendBilibiliCard(ctx, event, bilibiliSearchResultsTemplate, data, searchBilibiliUsersText(query, users))
}

// buildBilibiliSearchCardData 生成 UP 主搜索结果卡片的渲染输入。
func buildBilibiliSearchCardData(query string, users []bilibiliUser, commandPrefixes []string) map[string]any {
	cards := make([]map[string]any, 0, len(users))
	for index, user := range users {
		cards = append(cards, map[string]any{
			"rank":        index + 1,
			"name":        firstText(user.Name, user.UID),
			"uid_text":    uidText(user.UID),
			"avatar":      user.AvatarURL,
			"fans_text":   fansText(user.Fans),
			"videos_text": videosText(user.Videos),
			"level_icon":  bilibiliLevelIcon(user.Level, user.Senior),
			"verify_text": user.Verify,
			"verify_org":  user.VerifyOrg,
			"live":        user.Live,
			"sign":        truncateRunes(user.Sign, 48),
		})
	}
	return map[string]any{
		"query":       strings.TrimSpace(query),
		"subtitle":    "Bilibili · 订阅中心",
		"platform":    "Bilibili",
		"count":       len(cards),
		"users":       cards,
		"footer_hint": bilibiliSubscribeHint(commandPrefixes),
	}
}

// inlineBilibiliSearchAvatars 在进入 Chromium 前并发内联头像，避免远程图片阻塞页面加载。
// 单个头像失败或批次超过大小限制时使用模板内置占位头像，不再把远程 URL 交给渲染器。
func inlineBilibiliSearchAvatars(ctx context.Context, actions pluginActions, users []bilibiliUser) []bilibiliUser {
	resolved := append([]bilibiliUser(nil), users...)
	sizes := make([]int, len(resolved))
	semaphore := make(chan struct{}, maxBilibiliSearchAvatarConcurrency)
	var wait sync.WaitGroup

	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		resolved[index].AvatarURL = ""
		if sourceURL == "" {
			continue
		}

		wait.Add(1)
		go func(index int, sourceURL string) {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}

			dataURL, size, err := resolveAvatarDataURL(ctx, actions, sourceURL)
			if err != nil {
				return
			}
			resolved[index].AvatarURL = dataURL
			sizes[index] = size
		}(index, sourceURL)
	}

	wait.Wait()
	totalBytes := 0
	for index := range resolved {
		if resolved[index].AvatarURL == "" {
			continue
		}
		if totalBytes+sizes[index] > maxAvatarBatchBytes {
			resolved[index].AvatarURL = ""
			continue
		}
		totalBytes += sizes[index]
	}
	return resolved
}

func bilibiliSubscribeHint(commandPrefixes []string) string {
	for _, value := range commandPrefixes {
		prefix := strings.TrimSpace(value)
		if prefix != "" {
			return "使用前缀 " + prefix + "：" + prefix + "订阅b站推送 [类型] UID或昵称"
		}
	}
	return "使用“前缀 + 订阅b站推送 [类型] UID或昵称”"
}

// bilibiliLevelIcon 返回等级徽章资源路径；硬核会员使用带闪电的 LV6 徽章（与 B 站搜索页一致）。
func bilibiliLevelIcon(level int, senior bool) string {
	if senior {
		return "assets/lv6-senior.svg"
	}
	if level < 0 || level > 6 {
		return ""
	}
	return "assets/lv" + strconv.Itoa(level) + ".svg"
}

func videosText(videos int) string {
	if videos <= 0 {
		return ""
	}
	return "视频 " + formatCount(videos)
}
