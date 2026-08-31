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
	client := newDouyinClient(actions)
	shareBody := ""
	if ref.Kind == "short" {
		body, finalURL, err := client.requestShareHTML(ctx, ref.URL)
		if err != nil {
			return nil, errors.New(friendlyDouyinSourceError("抖音短链展开失败", err))
		}
		shareBody = body
		resolved := parseDouyinPreviewURL(finalURL)
		if resolved == nil || resolved.Kind == "short" {
			if id := douyinPreviewIDFromPage(body); id != "" {
				resolved = &douyinPreviewRef{Kind: "video", ID: id, URL: douyinCanonicalPreviewURL("video", id)}
			}
		}
		if resolved == nil || resolved.Kind == "short" || resolved.ID == "" {
			return nil, errors.New("没有从这条抖音短链解析到作品 ID")
		}
		resolved.URL = douyinCanonicalPreviewURL(resolved.Kind, resolved.ID)
		ref = resolved
	}
	if update := douyinPreviewUpdateFromPage(shareBody, ref); update != nil {
		return update, nil
	}
	accounts, accErr := readDouyinAccounts(ctx, actions)
	if ref.Kind == "live" {
		if accErr != nil {
			return nil, accErr
		}
		body, err := requestDouyinHTMLAcrossAccounts(ctx, actions, accounts, ref.URL, douyinWebReferer)
		if err != nil {
			return nil, errors.New(friendlyDouyinSourceError("抖音预览失败", err))
		}
		live := normalizeDouyinLive(douyinLiveFromPage(body), "")
		if live == nil {
			return nil, errors.New("没有找到这场抖音直播")
		}
		if rawURL := strings.TrimSpace(ref.URL); rawURL != "" {
			live["url"] = rawURL
		}
		return live, nil
	}
	var detailErr error
	if accErr == nil {
		if update, err := fetchDouyinDetailAcrossAccounts(ctx, client, accounts, ref); err == nil {
			return update, nil
		} else {
			detailErr = err
		}
	} else {
		detailErr = accErr
	}
	// 匿名链兜底：登录 CK 缺失或全部失败时，仅用匿名 ttwid 请求详情端点。
	// 分享页与详情端点携带 ttwid 即返回完整数据，登录 CK 不是必要条件。
	if update, err := fetchDouyinDetailAnonymous(ctx, client, ref); err == nil {
		return update, nil
	}
	if shareBody == "" {
		body, _, err := client.requestShareHTML(ctx, douyinSharePreviewURL(ref.Kind, ref.ID))
		if err == nil {
			shareBody = body
		} else if detailErr == nil {
			detailErr = err
		}
	}
	if update := douyinPreviewUpdateFromPage(shareBody, ref); update != nil {
		return update, nil
	}
	if detailErr != nil {
		return nil, errors.New(friendlyDouyinSourceError("抖音预览失败", detailErr))
	}
	if accErr != nil {
		return nil, accErr
	}
	return nil, errors.New("没有找到这条抖音作品")
}

func fetchDouyinDetailAnonymous(ctx context.Context, client *douyinClient, ref *douyinPreviewRef) (map[string]any, error) {
	endpoint := douyinAwemeDetailAPIURL(ref.ID)
	document, err := client.requestDetailJSONAnonymous(ctx, endpoint, ref.URL)
	if err != nil {
		return nil, err
	}
	for _, aweme := range douyinAwemesFromValue(document) {
		if update := douyinPreviewUpdateFromAweme(aweme, ref); update != nil {
			return update, nil
		}
	}
	return nil, &douyinSourceError{Kind: "invalid_response", HTTPStatus: 200, Endpoint: douyinEndpointPath(endpoint)}
}

func fetchDouyinDetailAcrossAccounts(ctx context.Context, client *douyinClient, accounts []douyinAccount, ref *douyinPreviewRef) (map[string]any, error) {
	endpoint := douyinAwemeDetailAPIURL(ref.ID)
	var lastError error
	for _, account := range accounts {
		document, err := client.requestDetailJSON(ctx, endpoint, account, ref.URL)
		if err != nil {
			lastError = err
			continue
		}
		for _, aweme := range douyinAwemesFromValue(document) {
			if update := douyinPreviewUpdateFromAweme(aweme, ref); update != nil {
				return update, nil
			}
		}
		lastError = &douyinSourceError{Kind: "invalid_response", HTTPStatus: 200, Endpoint: douyinEndpointPath(endpoint)}
	}
	if lastError == nil {
		lastError = errors.New("没有可用的抖音账号")
	}
	return nil, lastError
}

func douyinPreviewUpdateFromPage(body string, ref *douyinPreviewRef) map[string]any {
	for _, aweme := range douyinAwemesFromPage(body) {
		if update := douyinPreviewUpdateFromAweme(aweme, ref); update != nil {
			return update
		}
	}
	return nil
}

func douyinPreviewUpdateFromAweme(aweme map[string]any, ref *douyinPreviewRef) map[string]any {
	update := normalizeDouyinAweme(aweme)
	if update == nil || (ref.ID != "" && plugin.FirstText(update["id"]) != ref.ID && !strings.Contains(plugin.StringScalar(update["url"]), ref.ID)) {
		return nil
	}
	if rawURL := strings.TrimSpace(ref.URL); rawURL != "" {
		update["url"] = rawURL
	}
	return update
}

func douyinAwemeDetailAPIURL(awemeID string) string {
	values := douyinWebParams()
	values.Set("aweme_id", strings.TrimSpace(awemeID))
	return douyinAwemeDetailURL + "?" + values.Encode()
}

func douyinCanonicalPreviewURL(kind, id string) string {
	segment := "video"
	if kind == "image_text" {
		segment = "note"
	}
	return "https://www.douyin.com/" + segment + "/" + url.PathEscape(strings.TrimSpace(id))
}

func douyinSharePreviewURL(kind, id string) string {
	segment := "video"
	if kind == "image_text" {
		segment = "note"
	}
	return "https://www.iesdouyin.com/share/" + segment + "/" + url.PathEscape(strings.TrimSpace(id)) + "/"
}

func sampleDouyinUpdate(service string, now time.Time) map[string]any {
	base := map[string]any{
		"id": "preview-" + service, "platform": "douyin", "uid": "MS4wLjABAAAApreview", "service": service,
		"category": douyinServiceCategory(service),
		"author":   map[string]any{"name": "RayleaBot 示例用户", "uid": "MS4wLjABAAAApreview"},
		"pub_ts":   now.Unix(), "created_at": now.Format("2006-01-02 15:04"),
		"url":   "https://www.douyin.com/video/7000000000000000000",
		"stats": map[string]any{"play": 1286000, "like": 96000, "favorite": 68000, "comment": 8600, "share": 12000},
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
		base["stats"] = map[string]any{"viewers": 12680}
	default:
		base["title"], base["summary"], base["duration_text"] = "示例视频", "视频简介会显示在卡片正文区域，封面图显示在图片区域。", "1:20"
		base["images"] = []map[string]any{{"url": "assets/cover.svg", "width": 1280, "height": 720}}
	}
	return base
}
