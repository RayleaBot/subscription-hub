package plugin

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type weiboPreviewRef struct {
	ID  string
	URL string
}

var (
	weiboBidPattern     = regexp.MustCompile(`^[A-Za-z0-9]{6,16}$`)
	weiboReservedStatus = map[string]bool{
		"follow": true, "fans": true, "photo": true, "photos": true, "info": true,
		"home": true, "profile": true, "like": true, "avatar": true, "card": true,
		"followhim": true, "followme": true,
	}
	weiboReservedRoots = map[string]bool{
		"ttarticle": true, "tv": true, "signup": true, "login": true, "user": true,
		"search": true, "ajax": true, "note": true, "p": true,
	}
)

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
	parts := pathParts(parsed.Path)
	if len(parts) > 0 && weiboReservedRoots[strings.ToLower(parts[0])] {
		return nil
	}
	if id := weiboPreviewID(firstText(parsed.Query().Get("id"), parsed.Query().Get("mid"))); id != "" {
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
	case len(parts) == 2 && digits(parts[0]) != "" && weiboPreviewID(parts[1]) != "":
		id := weiboPreviewID(parts[1])
		return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
	case len(parts) >= 3 && parts[0] == "u" && digits(parts[1]) != "":
		if id := weiboPreviewID(parts[2]); id != "" {
			return &weiboPreviewRef{ID: id, URL: "https://m.weibo.cn/status/" + id}
		}
	}
	return nil
}

func looksLikeWeiboPreviewURL(value string) bool {
	canonical := normalizePreviewURL(value)
	parsed, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	return isWeiboPreviewHost(strings.ToLower(parsed.Hostname()))
}

func isWeiboPreviewHost(host string) bool {
	return host == "m.weibo.cn" || host == "weibo.cn" || host == "weibo.com" || host == "www.weibo.com" || strings.HasSuffix(host, ".weibo.com")
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

func fetchWeiboPreview(ctx context.Context, actions pluginActions, ref *weiboPreviewRef) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, weiboDetailTotalTimeout)
	defer cancel()
	endpoint := weiboStatusShowEndpoint(ref.ID)
	referer := "https://m.weibo.cn/status/" + ref.ID
	accounts, accErr := readWeiboAccounts(ctx, actions)
	if accErr != nil {
		accounts = []weiboAccount{{}}
	}
	document, err := requestWeiboAcrossAccounts(ctx, actions, accounts, endpoint, referer)
	if err != nil {
		logWeiboPreviewFailure(ctx, actions, "微博预览拉取失败", ref, err, "")
		if accErr != nil {
			return nil, accErr
		}
		return nil, errors.New(friendlyWeiboSourceError("微博预览失败", err))
	}
	mblog := weiboMblogFromShowDocument(document)
	update := normalizeWeiboMblog(mblog, 0)
	if update == nil {
		reason := weiboNormalizeSkipReason(mblog)
		logWeiboPreviewFailure(ctx, actions, "微博预览解析失败", ref, nil, reason)
		return nil, errors.New("没有找到这条微博")
	}
	if url := strings.TrimSpace(ref.URL); url != "" {
		update["url"] = url
	}
	prepared, _ := newWeiboLongTextResolver(actions, accounts).prepare(ctx, update)
	return prepared, nil
}

func logWeiboPreviewFailure(ctx context.Context, actions pluginActions, message string, ref *weiboPreviewRef, err error, reason string) {
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
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func weiboMblogFromShowDocument(document map[string]any) map[string]any {
	if document == nil {
		return nil
	}
	if data := mapValue(document["data"]); data != nil {
		if status := mapValue(data["status"]); weiboPreviewMblog(status) != nil {
			return status
		}
		if weiboPreviewMblog(data) != nil {
			return data
		}
	}
	if status := mapValue(document["status"]); weiboPreviewMblog(status) != nil {
		return status
	}
	return weiboPreviewMblog(document)
}

func weiboPreviewMblog(object map[string]any) map[string]any {
	if object == nil {
		return nil
	}
	if firstText(object["mid"], object["idstr"], object["id"], object["bid"]) == "" {
		return nil
	}
	return object
}

func sendWeiboPreview(ctx context.Context, event *rayleabot.EventContext, update map[string]any, realPreview bool) error {
	item := subscription{
		ID:       "preview-weibo-" + normalizedTargetType(event.Event.Target.Type) + "-" + firstText(event.Event.Target.ID, "current"),
		Platform: "weibo", UID: "6000000001", Name: "RayleaBot 示例博主",
		TargetType: normalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Services: []string{"all"}, Subscribers: mergeSubscriber(nil, event), Enabled: true,
	}
	if realPreview {
		author := mapValue(update["author"])
		item.UID = firstText(author["uid"], update["uid"], update["id"], "preview")
		item.Name = firstText(author["name"], "微博预览")
	}
	data := buildWeiboRenderData(item, update)
	inlineBilibiliUpdateAvatars(ctx, event.Actions(), data)
	if realPreview {
		inlineWeiboUpdateMedia(ctx, event.Actions(), data, nil)
	}
	imagePath, err := renderSubscriptionCardImage(ctx, event.Actions(), "weibo-update", data, buildWeiboFallback(data), previewRenderLogFields(update))
	if err != nil {
		return event.SendText(previewCardFailureText(err))
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func sampleWeiboUpdate(service string) map[string]any {
	now := time.Now()
	base := map[string]any{
		"id": "preview-" + service, "platform": "weibo", "uid": "6000000001", "service": service,
		"category": weiboServiceCategory(service),
		"author":   map[string]any{"name": "RayleaBot 示例博主", "uid": "6000000001"},
		"pub_ts":   now.Unix(), "created_at": now.Format("2006-01-02 15:04"),
		"url": "https://m.weibo.cn/status/5000000000000001",
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
