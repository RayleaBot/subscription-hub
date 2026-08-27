package bilibili

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const bilibiliSearchResultsTemplate = "bilibili-search-results"

const bilibiliSearchAvatarSuffix = "@96w_96h_1c.webp"

const bilibiliSearchAvatarTimeoutSeconds = 6

const bilibiliSearchFallbackAvatar = "assets/bilibili-default-avatar.gif"

const bilibiliSearchRenderErrorMessage = "Bilibili UP 搜索结果图片生成失败，请稍后重试。"

// buildBilibiliSearchCardData 生成 UP 主搜索结果卡片的渲染输入。
func buildBilibiliSearchCardData(query string, users []bilibiliUser, commandPrefixes []string) map[string]any {
	cards := make([]map[string]any, 0, len(users))
	for index, user := range users {
		cards = append(cards, map[string]any{
			"rank":        index + 1,
			"name":        plugin.FirstText(user.Name, user.UID),
			"uid_text":    plugin.UIDText(user.UID),
			"avatar":      user.AvatarURL,
			"fans_text":   plugin.FansText(user.Fans),
			"videos_text": videosText(user.Videos),
			"level_icon":  bilibiliLevelIcon(user.Level, user.Senior),
			"verify_text": user.Verify,
			"verify_org":  user.VerifyOrg,
			"live":        user.Live,
			"sign":        plugin.TruncateRunes(user.Sign, 48),
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
// 单个头像无法读取时使用模板内置的 Bilibili 官方默认头像，不阻断其余搜索结果。
func prepareBilibiliSearchAvatars(ctx context.Context, actions plugin.SourceActions, users []bilibiliUser) []bilibiliUser {
	resolved := append([]bilibiliUser(nil), users...)
	var wait sync.WaitGroup

	for index := range resolved {
		sourceURL := strings.TrimSpace(resolved[index].AvatarURL)
		resolved[index].AvatarURL = bilibiliSearchFallbackAvatar
		if sourceURL == "" || isBilibiliDefaultAvatarURL(sourceURL) {
			continue
		}
		requestURL, trusted := bilibiliSearchAvatarURL(sourceURL)
		if !trusted {
			continue
		}

		wait.Add(1)
		go func(index int, requestURL, sourceURL string) {
			defer wait.Done()
			candidates := []string{requestURL, sourceURL}
			for _, candidate := range candidates {
				dataURL, _, err := plugin.ResolveAvatarDataURLWithTimeout(ctx, actions, candidate, bilibiliSearchAvatarTimeoutSeconds, avatarPolicy())
				if err == nil {
					resolved[index].AvatarURL = dataURL
					return
				}
			}
		}(index, requestURL, sourceURL)
	}

	wait.Wait()
	return resolved
}

func isBilibiliDefaultAvatarURL(sourceURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	return err == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Fragment == "" && parsed.Port() == "" &&
		strings.EqualFold(parsed.Hostname(), "static.hdslb.com") && parsed.RawQuery == "" && parsed.EscapedPath() == "/images/member/noface.gif"
}

func bilibiliSearchAvatarURL(sourceURL string) (string, bool) {
	parsed, _, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy())
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
	return "视频 " + plugin.FormatCount(videos)
}
