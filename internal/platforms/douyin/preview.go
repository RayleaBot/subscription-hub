package douyin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

type douyinPreviewRef struct {
	Kind string
	ID   string
	URL  string
}

func parseDouyinPreviewURL(value string) *douyinPreviewRef {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	raw := value
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || !douyinAllowedRequestHost(parsed.Hostname()) {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	parts := plugin.PathParts(parsed.Path)
	if host == "v.douyin.com" || strings.HasSuffix(host, ".v.douyin.com") {
		if len(parts) == 0 {
			return nil
		}
		return &douyinPreviewRef{Kind: "short", ID: parts[0], URL: parsed.String()}
	}
	if host == "live.douyin.com" || strings.HasPrefix(host, "live.") {
		if len(parts) == 0 {
			return nil
		}
		return &douyinPreviewRef{Kind: "live", ID: parts[0], URL: parsed.String()}
	}
	for index, part := range parts {
		if index+1 >= len(parts) {
			continue
		}
		switch part {
		case "video":
			return &douyinPreviewRef{Kind: "video", ID: parts[index+1], URL: parsed.String()}
		case "note":
			return &douyinPreviewRef{Kind: "image_text", ID: parts[index+1], URL: parsed.String()}
		case "live":
			return &douyinPreviewRef{Kind: "live", ID: parts[index+1], URL: parsed.String()}
		}
	}
	return nil
}

func looksLikeDouyinPreviewURL(value string) bool {
	canonical := strings.TrimSpace(value)
	parsed, err := url.Parse(canonical)
	if err != nil {
		if !strings.Contains(canonical, "://") {
			parsed, err = url.Parse("https://" + canonical)
		}
		if err != nil {
			return false
		}
	}
	return douyinAllowedRequestHost(strings.ToLower(parsed.Hostname()))
}

func fetchDouyinPreview(ctx context.Context, actions plugin.SourceActions, ref *douyinPreviewRef) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, douyinDetailTotalTimeout)
	defer cancel()
	accounts, accErr := readDouyinAccounts(ctx, actions)
	if accErr != nil {
		accounts = []douyinAccount{{}}
	}
	body, err := requestDouyinHTMLAcrossAccounts(ctx, actions, accounts, ref.URL, douyinWebReferer)
	if err != nil {
		if accErr != nil {
			return nil, accErr
		}
		return nil, errors.New(friendlyDouyinSourceError("抖音预览失败", err))
	}
	if ref.Kind == "live" {
		live := normalizeDouyinLive(douyinLiveFromPage(body), "")
		if live == nil {
			return nil, errors.New("没有找到这场抖音直播")
		}
		if rawURL := strings.TrimSpace(ref.URL); rawURL != "" {
			live["url"] = rawURL
		}
		return live, nil
	}
	for _, aweme := range douyinAwemesFromPage(body) {
		update := normalizeDouyinAweme(aweme)
		if update == nil {
			continue
		}
		if ref.Kind != "short" && ref.ID != "" && plugin.FirstText(update["id"]) != ref.ID && !strings.Contains(plugin.StringScalar(update["url"]), ref.ID) {
			continue
		}
		if rawURL := strings.TrimSpace(ref.URL); rawURL != "" && ref.Kind != "short" {
			update["url"] = rawURL
		}
		return update, nil
	}
	return nil, errors.New("没有找到这条抖音作品")
}

func sampleDouyinUpdate(service string, now time.Time) map[string]any {
	base := map[string]any{
		"id": "preview-" + service, "platform": "douyin", "uid": "MS4wLjABAAAApreview", "service": service,
		"category": douyinServiceCategory(service),
		"author":   map[string]any{"name": "RayleaBot 示例用户", "uid": "MS4wLjABAAAApreview"},
		"pub_ts":   now.Unix(), "created_at": now.Format("2006-01-02 15:04"),
		"url": "https://www.douyin.com/video/7000000000000000000",
	}
	switch service {
	case "image_text":
		base["summary"] = "这里展示图文作品正文、图片九宫格、作者信息和订阅人身份。"
		base["images"] = []map[string]any{
			{"url": "assets/grid.svg"},
			{"url": "assets/grid.svg"},
			{"url": "assets/grid.svg"},
		}
	case "live":
		base["title"], base["summary"] = "示例直播间", "直播封面会显示在卡片图片区域。"
		base["url"] = "https://live.douyin.com/123456"
		base["images"] = []map[string]any{{"url": "assets/cover.svg"}}
	default:
		base["title"], base["summary"], base["duration_text"] = "示例视频", "视频简介会显示在卡片正文区域，封面图显示在图片区域。", "1:20"
		base["images"] = []map[string]any{{"url": "assets/cover.svg", "width": 1280, "height": 720}}
	}
	return base
}
