package plugin

import (
	"context"
	"strconv"
	"strings"
	"sync"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliSearchResultsTemplate = "bilibili-search-results"
	bilibiliSearchAvatarSuffix    = "@96w_96h_1c.webp"
)

// replyBilibiliUserSearch 搜索 UP 主并回复结果卡片；渲染失败时降级为文字列表。
func replyBilibiliUserSearch(ctx context.Context, event *rayleabot.EventContext, query string) error {
	users, err := searchBilibili(ctx, event, query)
	if err != nil {
		return event.SendText(friendlyBilibiliError(err))
	}
	renderUsers, remoteUsers := prepareBilibiliSearchAvatars(ctx, event.Actions(), users)
	data := buildBilibiliSearchCardData(query, renderUsers, event.CommandPrefixes)
	var renderFallback map[string]any
	if hasInlineBilibiliSearchAvatar(renderUsers) {
		renderFallback = buildBilibiliSearchCardData(query, remoteUsers, event.CommandPrefixes)
	}
	return sendBilibiliCardWithRenderFallback(ctx, event, bilibiliSearchResultsTemplate, data, renderFallback, searchBilibiliUsersText(query, users))
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
// 单个头像无法内联时保留经过校验的源地址，供渲染器继续加载。
func prepareBilibiliSearchAvatars(ctx context.Context, actions pluginActions, users []bilibiliUser) ([]bilibiliUser, []bilibiliUser) {
	resolved := append([]bilibiliUser(nil), users...)
	remote := append([]bilibiliUser(nil), users...)
	var wait sync.WaitGroup

	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		if sourceURL == "" {
			continue
		}
		requestURL, trusted := bilibiliSearchAvatarURL(sourceURL)
		if !trusted {
			resolved[index].AvatarURL = ""
			remote[index].AvatarURL = ""
			continue
		}
		resolved[index].AvatarURL = sourceURL
		remote[index].AvatarURL = sourceURL

		wait.Add(1)
		go func(index int, requestURL string) {
			defer wait.Done()
			dataURL, _, err := resolveAvatarDataURL(ctx, actions, requestURL)
			if err != nil {
				return
			}
			resolved[index].AvatarURL = dataURL
		}(index, requestURL)
	}

	wait.Wait()
	return resolved, remote
}

func hasInlineBilibiliSearchAvatar(users []bilibiliUser) bool {
	for _, user := range users {
		if strings.HasPrefix(strings.TrimSpace(user.AvatarURL), "data:image/") {
			return true
		}
	}
	return false
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
