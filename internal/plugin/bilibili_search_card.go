package plugin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliSearchResultsTemplate      = "bilibili-search-results"
	bilibiliSearchAvatarSuffix         = "@96w_96h_1c.webp"
	bilibiliSearchAvatarTimeoutSeconds = 6
	bilibiliSearchAvatarErrorMessage   = "Bilibili UP 头像获取失败，请稍后重试。"
	bilibiliSearchRenderErrorMessage   = "Bilibili UP 搜索结果图片生成失败，请稍后重试。"
)

// replyBilibiliUserSearch 搜索 UP 主并回复包含头像的结果卡片。
func replyBilibiliUserSearch(ctx context.Context, event *rayleabot.EventContext, query string) error {
	users, err := searchBilibili(ctx, event, query)
	if err != nil {
		return event.SendText(friendlyBilibiliError(err))
	}
	renderUsers, err := prepareBilibiliSearchAvatars(ctx, event.Actions(), users)
	if err != nil {
		return event.SendText(bilibiliSearchAvatarErrorMessage)
	}
	data := buildBilibiliSearchCardData(query, renderUsers, event.CommandPrefixes)
	return sendBilibiliCard(ctx, event, bilibiliSearchResultsTemplate, data, bilibiliSearchRenderErrorMessage)
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

// prepareBilibiliSearchAvatars 并发解析全部搜索结果的紧凑内联头像。
func prepareBilibiliSearchAvatars(ctx context.Context, actions pluginActions, users []bilibiliUser) ([]bilibiliUser, error) {
	resolved := append([]bilibiliUser(nil), users...)
	failed := make([]bool, len(resolved))
	var wait sync.WaitGroup

	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		resolved[index].AvatarURL = ""
		if sourceURL == "" {
			failed[index] = true
			continue
		}
		requestURL, trusted := bilibiliSearchAvatarURL(sourceURL)
		if !trusted {
			failed[index] = true
			continue
		}

		wait.Add(1)
		go func(index int, requestURL, sourceURL string) {
			defer wait.Done()
			candidates := []string{requestURL, sourceURL}
			for _, candidate := range candidates {
				dataURL, _, err := resolveAvatarDataURLWithTimeout(ctx, actions, candidate, bilibiliSearchAvatarTimeoutSeconds)
				if err == nil {
					resolved[index].AvatarURL = dataURL
					return
				}
			}
			failed[index] = true
		}(index, requestURL, sourceURL)
	}

	wait.Wait()
	failureCount := 0
	for _, itemFailed := range failed {
		if itemFailed {
			failureCount++
		}
	}
	if failureCount > 0 {
		return resolved, fmt.Errorf("resolve %d Bilibili search avatars", failureCount)
	}
	return resolved, nil
}

func bilibiliSearchAvatarURL(sourceURL string) (string, bool) {
	parsed, _, err := validateAvatarSourceURL(sourceURL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "i0.hdslb.com" && host != "i1.hdslb.com" && host != "i2.hdslb.com" {
		return parsed.String(), true
	}
	if !strings.Contains(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:], "@") {
		parsed.Path += bilibiliSearchAvatarSuffix
		parsed.RawPath = ""
	}
	return parsed.String(), true
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
