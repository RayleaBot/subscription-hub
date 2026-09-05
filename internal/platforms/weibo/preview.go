package weibo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

type weiboPreviewRef struct {
	ID  string
	URL string
}

var weiboBidPattern = regexp.MustCompile(`^[A-Za-z0-9]{6,16}$`)

var weiboReservedStatus = map[string]bool{
	"follow": true, "fans": true, "photo": true, "photos": true, "info": true,
	"home": true, "profile": true, "like": true, "avatar": true, "card": true,
	"followhim": true, "followme": true,
}

var weiboReservedRoots = map[string]bool{
	"ttarticle": true, "tv": true, "signup": true, "login": true, "user": true,
	"search": true, "ajax": true, "note": true, "p": true,
}

func parseWeiboPreviewURL(value string) *weiboPreviewRef {
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
	if err != nil || !isWeiboPreviewHost(strings.ToLower(parsed.Hostname())) {
		return nil
	}
	parts := plugin.PathParts(parsed.Path)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "tv") && strings.EqualFold(parts[1], "show") {
		mid := strings.TrimSpace(parsed.Query().Get("mid"))
		if weiboNumericIDPattern.MatchString(mid) {
			mid = weiboMIDToBID(mid)
		}
		if id := weiboPreviewID(mid); id != "" {
			return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
		}
	}
	if len(parts) > 0 && weiboReservedRoots[strings.ToLower(parts[0])] {
		return nil
	}
	if id := weiboPreviewID(plugin.FirstText(parsed.Query().Get("id"), parsed.Query().Get("mid"))); id != "" {
		return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
	}
	switch {
	case len(parts) >= 2 && (parts[0] == "status" || parts[0] == "detail"):
		if id := weiboPreviewID(parts[1]); id != "" {
			return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
		}
	case len(parts) >= 2 && parts[0] == "statuses" && parts[1] == "show":
		if id := weiboPreviewID(parts[len(parts)-1]); id != "" && parts[len(parts)-1] != "show" {
			return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
		}
	case len(parts) == 2 && plugin.Digits(parts[0]) != "" && weiboPreviewID(parts[1]) != "":
		id := weiboPreviewID(parts[1])
		return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
	case len(parts) >= 3 && parts[0] == "u" && plugin.Digits(parts[1]) != "":
		if id := weiboPreviewID(parts[2]); id != "" {
			return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
		}
	}
	return nil
}

func looksLikeWeiboPreviewURL(value string) bool {
	canonical := plugin.NormalizePreviewURL(value)
	parsed, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	return isWeiboPreviewHost(strings.ToLower(parsed.Hostname()))
}

func isWeiboPreviewHost(host string) bool {
	return host == "m.weibo.cn" || host == "weibo.cn" || host == "weibo.com" || host == "www.weibo.com" || strings.HasSuffix(host, ".weibo.com")
}

func isWeiboShortURL(value string) bool {
	parsed, err := url.Parse(plugin.NormalizePreviewURL(value))
	return err == nil && strings.EqualFold(parsed.Hostname(), "t.cn")
}

func weiboPreviewID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || weiboReservedStatus[strings.ToLower(value)] {
		return ""
	}
	if weiboNumericIDPattern.MatchString(value) || weiboBidPattern.MatchString(value) {
		return value
	}
	return ""
}

func weiboMIDToBID(mid string) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	parts := make([]string, 0, (len(mid)+6)/7)
	for end := len(mid); end > 0; {
		start := end - 7
		if start < 0 {
			start = 0
		}
		value, err := strconv.ParseUint(mid[start:end], 10, 32)
		if err != nil {
			return ""
		}
		encoded := "0"
		if value > 0 {
			encoded = ""
			for value > 0 {
				encoded = string(alphabet[value%62]) + encoded
				value /= 62
			}
		}
		if start > 0 && len(encoded) < 4 {
			encoded = strings.Repeat("0", 4-len(encoded)) + encoded
		}
		parts = append(parts, encoded)
		end = start
	}
	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}
	return strings.Join(parts, "")
}

func fetchWeiboPreview(ctx context.Context, actions plugin.SourceActions, ref *weiboPreviewRef) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, weiboDetailTotalTimeout)
	defer cancel()
	endpoint := weiboStatusShowEndpoint(ref.ID)
	referer := "https://m.weibo.cn/status/" + ref.ID
	accounts, accErr := readWeiboAccounts(ctx, actions)
	if accErr != nil {
		accounts = []weiboAccount{{}}
	}
	document, requestErr := requestWeiboAcrossAccounts(ctx, actions, accounts, endpoint, referer)
	mblog := weiboMblogFromShowDocument(document)
	update := normalizeWeiboMblog(mblog, 0)
	if update == nil {
		if detailMblog, err := requestWeiboDetailAcrossAccounts(ctx, actions, accounts, ref.ID); err == nil {
			mblog = detailMblog
			update = normalizeWeiboMblog(mblog, 0)
		}
	}
	if update == nil {
		if requestErr != nil {
			logWeiboPreviewFailure(ctx, actions, "微博预览拉取失败", ref, requestErr, "")
			if accErr != nil {
				return nil, accErr
			}
			return nil, errors.New(friendlyWeiboSourceError("微博预览失败", requestErr))
		}
		reason := weiboNormalizeSkipReason(mblog)
		logWeiboPreviewFailure(ctx, actions, "微博预览解析失败", ref, nil, reason)
		return nil, errors.New("没有找到这条微博")
	}
	if url := strings.TrimSpace(ref.URL); url != "" {
		update["url"] = url
	}
	prepared, _ := newWeiboLongTextResolver(actions, accounts).Prepare(ctx, update)
	return prepared, nil
}

func requestWeiboDetailAcrossAccounts(ctx context.Context, actions plugin.SourceActions, accounts []weiboAccount, id string) (map[string]any, error) {
	client := newWeiboClient(actions)
	var lastError error
	for _, account := range accounts {
		page, err := client.requestHTML(ctx, weiboDetailEndpoint(id), account, "https://m.weibo.cn/status/"+id)
		if err != nil {
			lastError = err
			continue
		}
		if mblog := weiboMblogFromDetailPage(page); mblog != nil {
			return mblog, nil
		}
		lastError = errors.New("微博详情页没有可解析的数据")
	}
	if lastError == nil {
		lastError = errors.New("没有可用的微博账号")
	}
	return nil, lastError
}

func weiboMblogFromDetailPage(page string) map[string]any {
	for offset := 0; offset < len(page); {
		index := strings.Index(page[offset:], `"status"`)
		if index < 0 {
			return nil
		}
		index += offset + len(`"status"`)
		start := index
		for start < len(page) && (page[start] == ' ' || page[start] == '\t' || page[start] == '\r' || page[start] == '\n') {
			start++
		}
		if start >= len(page) || page[start] != ':' {
			offset = index
			continue
		}
		start++
		for start < len(page) && (page[start] == ' ' || page[start] == '\t' || page[start] == '\r' || page[start] == '\n') {
			start++
		}
		if start < len(page) && page[start] == '{' {
			var status map[string]any
			if err := json.NewDecoder(strings.NewReader(page[start:])).Decode(&status); err == nil && weiboPreviewMblog(status) != nil {
				return status
			}
		}
		offset = index
	}
	return nil
}

func logWeiboPreviewFailure(ctx context.Context, actions plugin.SourceActions, message string, ref *weiboPreviewRef, err error, reason string) {
	if actions == nil {
		return
	}
	fields := map[string]any{}
	if ref != nil {
		fields["weibo_id"] = ref.ID
	}
	for key, value := range weiboErrorLogFields(err) {
		fields[key] = value
	}
	if reason != "" {
		fields["reason"] = reason
	}
	weiboID := "未知内容"
	if ref != nil && strings.TrimSpace(ref.ID) != "" {
		weiboID = strings.TrimSpace(ref.ID)
	}
	completeMessage := fmt.Sprintf("%s（%s）", message, weiboID)
	if err != nil {
		completeMessage += strings.TrimSpace(friendlyWeiboSourceError("原因", err))
	} else if strings.TrimSpace(reason) != "" {
		completeMessage += "原因：" + strings.TrimSpace(reason) + "。"
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: completeMessage, Fields: fields})
}

func weiboMblogFromShowDocument(document map[string]any) map[string]any {
	if document == nil {
		return nil
	}
	if data := plugin.MapValue(document["data"]); data != nil {
		if status := plugin.MapValue(data["status"]); weiboPreviewMblog(status) != nil {
			return status
		}
		if weiboPreviewMblog(data) != nil {
			return data
		}
	}
	if status := plugin.MapValue(document["status"]); weiboPreviewMblog(status) != nil {
		return status
	}
	return weiboPreviewMblog(document)
}

func weiboPreviewMblog(object map[string]any) map[string]any {
	if object == nil {
		return nil
	}
	if plugin.FirstText(object["mid"], object["idstr"], object["id"], object["bid"]) == "" {
		return nil
	}
	return object
}

func sampleWeiboUpdate(service string, now time.Time) map[string]any {
	base := map[string]any{
		"id": "preview-" + service, "platform": "weibo", "uid": "6000000001", "service": service,
		"category": weiboServiceCategory(service),
		"author":   map[string]any{"name": "RayleaBot 示例博主", "uid": "6000000001"},
		"pub_ts":   now.Unix(), "created_at": now.Format("2006-01-02 15:04"),
		"url":   "https://m.weibo.cn/status/5000000000000001",
		"stats": map[string]any{"repost": 12000, "comment": 8600, "like": 96000},
	}
	switch service {
	case "image":
		base["summary"] = "这里展示图片微博正文、图片九宫格、博主信息和订阅人身份。"
		base["images"] = []map[string]any{
			{"url": "assets/grid.svg"},
			{"url": "assets/grid.svg"},
			{"url": "assets/grid.svg"},
		}
	case "video":
		base["title"], base["summary"], base["duration_text"] = "视频微博示例", "视频简介会显示在卡片正文区域，封面图显示在图片区域。", "1:20"
		base["images"] = []map[string]any{{"url": "assets/cover.svg", "width": 1280, "height": 720}}
	case "repost":
		base["summary"] = "转发评论会显示在主卡片正文中。"
		base["images"] = []map[string]any{{"url": "assets/grid.svg"}}
		base["original"] = map[string]any{
			"id": "preview-original", "service": "image", "category": "图片微博",
			"summary": "原微博摘要会以内嵌卡片显示，包含原作者、正文、图片和链接。",
			"author":  map[string]any{"name": "原微博作者", "uid": "6000000002"},
			"images":  []map[string]any{{"url": "assets/grid.svg"}},
			"url":     "https://m.weibo.cn/status/5000000000000000", "created_at": now.Add(-5 * time.Minute).Format("2006-01-02 15:04"),
		}
	default:
		base["service"], base["category"] = "post", weiboServiceCategory("post")
		base["summary"] = "文字微博会显示博主、正文、链接和订阅人，不附带图片。"
		base["images"] = []map[string]any{}
	}
	return base
}
