package plugin

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type bilibiliPreviewRef struct {
	Kind string
	ID   string
	URL  string
}

func parseBilibiliPreviewURL(value string) *bilibiliPreviewRef {
	canonical := normalizePreviewURL(value)
	if canonical == "" {
		return nil
	}
	parsed, _ := url.Parse(canonical)
	host := strings.ToLower(parsed.Hostname())
	parts := pathParts(parsed.Path)
	switch {
	case isBilibiliContentHost(host) && len(parts) == 2 && parts[0] == "video" && bilibiliBVIDPattern.MatchString(parts[1]):
		return &bilibiliPreviewRef{Kind: "video", ID: parts[1], URL: "https://www.bilibili.com/video/" + parts[1]}
	case isBilibiliContentHost(host) && len(parts) == 2 && parts[0] == "opus" && digits(parts[1]) != "":
		return &bilibiliPreviewRef{Kind: "opus", ID: parts[1], URL: "https://www.bilibili.com/opus/" + parts[1]}
	case host == "t.bilibili.com" && len(parts) == 1 && digits(parts[0]) != "":
		return &bilibiliPreviewRef{Kind: "dynamic", ID: parts[0], URL: "https://t.bilibili.com/" + parts[0]}
	case host == "live.bilibili.com" && len(parts) >= 1 && digits(parts[0]) != "":
		return &bilibiliPreviewRef{Kind: "live", ID: parts[0], URL: "https://live.bilibili.com/" + parts[0]}
	default:
		return nil
	}
}

func normalizePreviewURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	} else if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	parsed.Scheme = "https"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func looksLikeBilibiliPreviewURL(value string) bool {
	canonical := normalizePreviewURL(value)
	parsed, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return isBilibiliContentHost(host) || host == "t.bilibili.com" || host == "live.bilibili.com"
}

func isBilibiliContentHost(host string) bool {
	return host == "www.bilibili.com" || host == "m.bilibili.com" || host == "bilibili.com"
}

func previewSubscriptionCard(ctx context.Context, event *rayleabot.EventContext, input string) error {
	input = strings.TrimSpace(input)
	if douyinRef := parseDouyinPreviewURL(input); douyinRef != nil {
		update, err := fetchDouyinPreview(ctx, event.Actions(), douyinRef)
		if err != nil {
			return event.SendText(ensureSentence(err.Error()))
		}
		return sendDouyinPreview(ctx, event, update, true)
	}
	if looksLikeDouyinPreviewURL(input) {
		return event.SendText("暂不支持这个抖音链接。")
	}
	if weiboRef := parseWeiboPreviewURL(input); weiboRef != nil {
		update, err := fetchWeiboPreview(ctx, event.Actions(), weiboRef)
		if err != nil {
			return event.SendText(ensureSentence(err.Error()))
		}
		return sendWeiboPreview(ctx, event, update, true)
	}
	if looksLikeWeiboPreviewURL(input) {
		return event.SendText("暂不支持这个微博链接。")
	}
	previewRef := parseBilibiliPreviewURL(input)
	if previewRef != nil {
		update, err := fetchBilibiliPreview(ctx, event.Actions(), previewRef)
		if err != nil {
			return event.SendText(ensureSentence(err.Error()))
		}
		return sendBilibiliPreview(ctx, event, update, true)
	}
	if looksLikeBilibiliPreviewURL(input) {
		return event.SendText("暂不支持这个 Bilibili 链接。")
	}
	platform, service := parsePreviewInput(input)
	if platform == "douyin" {
		return sendDouyinPreview(ctx, event, sampleDouyinUpdate(service), false)
	}
	if platform == "weibo" {
		return sendWeiboPreview(ctx, event, sampleWeiboUpdate(service), false)
	}
	return sendBilibiliPreview(ctx, event, sampleBilibiliUpdate(service), false)
}

func parsePreviewInput(input string) (string, string) {
	input = strings.TrimSpace(input)
	if platform, rest, ok := splitPreviewPlatform(input); ok {
		switch platform {
		case "weibo":
			service := normalizeService(rest, "weibo")
			if service == "" || service == "all" {
				service = "post"
			}
			return "weibo", service
		case "douyin":
			service := normalizeService(rest, "douyin")
			if service == "" || service == "all" {
				service = "video"
			}
			return "douyin", service
		}
		service := normalizeService(rest, "bilibili")
		if service == "" || service == "all" {
			service = "video"
		}
		return "bilibili", service
	}
	if service := uniqueWeiboPreviewService(input); service != "" {
		return "weibo", service
	}
	service := normalizeService(input, "bilibili")
	if service == "" || service == "all" {
		service = "video"
	}
	return "bilibili", service
}

func splitPreviewPlatform(input string) (string, string, bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", "", false
	}
	first, lower := fields[0], strings.ToLower(fields[0])
	switch {
	case first == "微博" || lower == "weibo":
		return "weibo", strings.Join(fields[1:], " "), true
	case first == "b站" || first == "B站" || lower == "bilibili" || lower == "bili":
		return "bilibili", strings.Join(fields[1:], " "), true
	case first == "抖音" || lower == "douyin":
		return "douyin", strings.Join(fields[1:], " "), true
	}
	if strings.HasPrefix(input, "微博") && input != "微博" {
		return "weibo", strings.TrimSpace(strings.TrimPrefix(input, "微博")), true
	}
	if len(lower) > 5 && strings.HasPrefix(lower, "weibo") {
		return "weibo", strings.TrimSpace(input[5:]), true
	}
	if strings.HasPrefix(input, "抖音") && input != "抖音" {
		return "douyin", strings.TrimSpace(strings.TrimPrefix(input, "抖音")), true
	}
	if len(lower) > 6 && strings.HasPrefix(lower, "douyin") {
		return "douyin", strings.TrimSpace(input[6:]), true
	}
	if strings.HasPrefix(input, "b站") && input != "b站" {
		return "bilibili", strings.TrimSpace(strings.TrimPrefix(input, "b站")), true
	}
	if strings.HasPrefix(input, "B站") && input != "B站" {
		return "bilibili", strings.TrimSpace(strings.TrimPrefix(input, "B站")), true
	}
	return "", "", false
}

func uniqueWeiboPreviewService(input string) string {
	if normalizeService(input, "bilibili") != "" {
		return ""
	}
	switch normalizeService(input, "weibo") {
	case "image":
		return "image"
	case "post":
		if input == "文字" {
			return "post"
		}
	}
	return ""
}

func fetchBilibiliPreview(ctx context.Context, actions pluginActions, ref *bilibiliPreviewRef) (map[string]any, error) {
	account := bilibiliAccount{}
	if accounts, err := readBilibiliAccounts(ctx, actions); err == nil && len(accounts) > 0 {
		account = accounts[0]
	}
	client := newBilibiliClient(actions)
	var endpoint string
	switch ref.Kind {
	case "video":
		endpoint = bilibiliVideoViewURL + "?" + url.Values{"bvid": []string{ref.ID}}.Encode()
		document, err := client.requestJSON(ctx, "GET", endpoint, account, false, false, "", false)
		if err != nil {
			return nil, errors.New(friendlyBilibiliSourceError("Bilibili 视频预览失败", err))
		}
		return previewVideoUpdate(document, ref.URL)
	case "opus":
		endpoint = bilibiliOpusDetailURL + "?" + url.Values{"id": []string{ref.ID}}.Encode()
		document, err := client.requestJSON(ctx, "GET", endpoint, account, false, false, "", false)
		if err != nil {
			return nil, errors.New(friendlyBilibiliSourceError("Bilibili 动态预览失败", err))
		}
		return previewDynamicUpdate(document, ref.URL)
	case "dynamic":
		values := url.Values{"id": []string{ref.ID}, "features": []string{"itemOpusStyle,opusBigCover,onlyfansVote,decorationCard,onlyfansAssetsV2,forwardListHidden,ugcDelete"}}
		endpoint = bilibiliDynamicURL + "?" + values.Encode()
		document, err := client.requestJSON(ctx, "GET", endpoint, account, false, false, "", false)
		if err != nil {
			return nil, errors.New(friendlyBilibiliSourceError("Bilibili 动态预览失败", err))
		}
		return previewDynamicUpdate(document, ref.URL)
	case "live":
		endpoint = bilibiliLiveRoomURL + "?" + url.Values{"room_id": []string{ref.ID}}.Encode()
		document, err := client.requestJSON(ctx, "GET", endpoint, account, false, true, "", false)
		if err != nil {
			return nil, errors.New(friendlyBilibiliSourceError("Bilibili 直播间预览失败", err))
		}
		uid := stringScalar(nestedValue(document, "data", "uid"))
		var statusDocument map[string]any
		if uid != "" {
			statusDocument, _ = client.requestJSON(ctx, "GET", bilibiliLiveStatusEndpoint([]string{uid}), account, false, true, "", false)
		}
		return previewLiveUpdate(document, statusDocument, ref.URL, ref.ID)
	default:
		return nil, errors.New("暂不支持这个 Bilibili 链接")
	}
}

func previewVideoUpdate(document map[string]any, canonicalURL string) (map[string]any, error) {
	data := mapValue(document["data"])
	if data == nil {
		return nil, errors.New("Bilibili 视频预览失败：响应格式不正确")
	}
	owner := mapValue(data["owner"])
	id := firstText(data["bvid"], data["aid"])
	if id == "" {
		parts := pathParts(canonicalURL)
		if len(parts) > 0 {
			id = parts[len(parts)-1]
		}
	}
	images := make([]map[string]any, 0, 1)
	appendBilibiliImage(&images, data["pic"])
	return map[string]any{
		"id": id, "service": "video", "category": dynamicCategory("video"),
		"title": firstCleanText(data["title"], "Bilibili 视频预览"), "summary": truncateRunes(firstCleanText(data["desc"], data["dynamic"]), 420),
		"url": canonicalURL, "pub_ts": intScalar(firstNonNil(data["pubdate"], data["ctime"])),
		"created_at":    formatBilibiliTime(intScalar(firstNonNil(data["pubdate"], data["ctime"])), ""),
		"duration_text": formatVideoDuration(intScalar(data["duration"])),
		"author":        map[string]any{"name": cleanText(owner["name"]), "avatar": normalizeBilibiliURL(owner["face"]), "uid": cleanText(owner["mid"])},
		"images":        images,
	}, nil
}

func previewDynamicUpdate(document map[string]any, canonicalURL string) (map[string]any, error) {
	data := mapValue(document["data"])
	if data == nil {
		return nil, errors.New("Bilibili 动态预览失败：响应格式不正确")
	}
	var item map[string]any
	for _, key := range []string{"item", "opus", "dynamic"} {
		candidate := mapValue(data[key])
		if candidate != nil && candidate["modules"] != nil {
			item = candidate
			break
		}
	}
	if item == nil && data["modules"] != nil {
		item = data
	}
	if item == nil {
		for _, raw := range sliceValue(data["items"]) {
			candidate := mapValue(raw)
			if candidate != nil && candidate["modules"] != nil {
				item = candidate
				break
			}
		}
	}
	if item == nil {
		return nil, errors.New("Bilibili 动态预览失败：未识别到可预览的动态内容")
	}
	update := normalizeDynamicItem(item, 0)
	if update == nil {
		update = normalizeOpusDetailItem(item, canonicalURL)
	}
	if update == nil {
		return nil, errors.New("Bilibili 动态预览失败：未识别到可预览的动态内容")
	}
	update["url"] = canonicalURL
	return update, nil
}

func normalizeOpusDetailItem(item map[string]any, canonicalURL string) map[string]any {
	modules := sliceValue(item["modules"])
	if len(modules) == 0 {
		return nil
	}
	basic := mapValue(item["basic"])
	author := map[string]any{}
	title := cleanBilibiliTitle(basic["title"])
	summaryParts := make([]string, 0)
	htmlParts := make([]string, 0)
	images := make([]map[string]any, 0)
	topic := topicFromValue(firstNonNil(item["topic"], basic["topic"]))
	for _, raw := range modules {
		module := mapValue(raw)
		switch stringScalar(module["module_type"]) {
		case "MODULE_TYPE_AUTHOR":
			author = opusDetailAuthor(mapValue(module["module_author"]))
		case "MODULE_TYPE_TITLE":
			if text := cleanText(nestedValue(module, "module_title", "text")); text != "" {
				title = text
			}
		case "MODULE_TYPE_CONTENT":
			text, rendered, contentImages := opusDetailContent(mapValue(module["module_content"]))
			if text != "" {
				summaryParts = append(summaryParts, text)
			}
			if rendered != "" {
				htmlParts = append(htmlParts, rendered)
			}
			images = append(images, contentImages...)
		case "MODULE_TYPE_TOPIC":
			if value := topicFromValue(module["module_topic"]); value != nil {
				topic = value
			}
		}
	}
	authorName := stringScalar(author["name"])
	if title == "" {
		if authorName != "" {
			title = authorName + " 发布图文动态"
		} else {
			title = "图文动态更新"
		}
	}
	id := firstText(item["id_str"], item["id"], basic["comment_id_str"], basic["rid_str"])
	if id == "" {
		parts := pathParts(canonicalURL)
		if len(parts) > 0 {
			id = parts[len(parts)-1]
		}
	}
	if id == "" {
		return nil
	}
	pubTS := intScalar(firstNonNil(author["pub_ts"], basic["pub_ts"]))
	if len(images) > 9 {
		images = images[:9]
	}
	return map[string]any{
		"id": id, "type": firstText(item["type"], "DYNAMIC_TYPE_DRAW"), "service": "image_text", "category": dynamicCategory("image_text"),
		"title": title, "summary": truncateRunes(strings.Join(summaryParts, "\n"), 420), "summary_html": strings.Join(htmlParts, "<br>"),
		"url": canonicalURL, "pub_ts": pubTS, "created_at": formatBilibiliTime(pubTS, stringScalar(author["pub_time"])),
		"author": map[string]any{"name": authorName, "avatar": normalizeBilibiliURL(author["avatar"]), "uid": firstText(author["uid"], basic["uid"])},
		"images": images, "topic": topic, "is_pinned": false, "original": nil,
	}
}

func opusDetailAuthor(author map[string]any) map[string]any {
	if author == nil {
		return map[string]any{}
	}
	avatar := firstNonNil(author["face"], nestedValue(author, "avatar", "fallback_layers", "layers", 0, "resource", "res_image", "image_src", "remote", "url"))
	return map[string]any{
		"name": cleanText(author["name"]), "avatar": avatar, "uid": cleanText(author["mid"]),
		"pub_ts": author["pub_ts"], "pub_time": author["pub_time"],
	}
}

func opusDetailContent(content map[string]any) (string, string, []map[string]any) {
	textParts := make([]string, 0)
	htmlParts := make([]string, 0)
	images := make([]map[string]any, 0)
	for _, raw := range sliceValue(content["paragraphs"]) {
		paragraph := mapValue(raw)
		nodes := make([]any, 0)
		nodes = append(nodes, sliceValue(nestedValue(paragraph, "text", "nodes"))...)
		nodes = append(nodes, sliceValue(nestedValue(paragraph, "heading", "nodes"))...)
		paragraphText := make([]string, 0, len(nodes))
		var rendered strings.Builder
		for _, nodeRaw := range nodes {
			node := mapValue(nodeRaw)
			word := mapValue(node["word"])
			rich := mapValue(node["rich"])
			text := firstCleanText(word["words"], rich["text"], rich["orig_text"])
			if text != "" {
				paragraphText = append(paragraphText, text)
			}
			if stringScalar(node["type"]) == "TEXT_NODE_TYPE_RICH" {
				rendered.WriteString(richTextNodeHTML(rich, true))
			} else {
				rendered.WriteString(strings.ReplaceAll(htmlEscape(firstText(word["words"], text)), "\n", "<br>"))
			}
		}
		if len(paragraphText) > 0 {
			textParts = append(textParts, strings.Join(paragraphText, " "))
		}
		if rendered.Len() > 0 {
			htmlParts = append(htmlParts, rendered.String())
		}
		for _, image := range sliceValue(nestedValue(paragraph, "pic", "pics")) {
			appendBilibiliImage(&images, image)
		}
	}
	return strings.Join(textParts, "\n"), strings.Join(htmlParts, "<br>"), images
}

func previewLiveUpdate(document, statusDocument map[string]any, canonicalURL, roomID string) (map[string]any, error) {
	data := mapValue(document["data"])
	if data == nil {
		return nil, errors.New("Bilibili 直播间预览失败：响应格式不正确")
	}
	uid := stringScalar(data["uid"])
	statusEntry := mapValue(nestedValue(statusDocument, "data", uid))
	pubTS := liveStartTimestamp(data)
	if pubTS == 0 {
		pubTS = liveStartTimestamp(statusEntry)
	}
	status := intScalar(data["live_status"])
	if _, exists := data["live_status"]; !exists {
		status = intScalar(statusEntry["live_status"])
	}
	images := liveCoverImages(data)
	if len(images) == 0 {
		images = liveCoverImages(statusEntry)
	}
	startedAt := formatBilibiliTime(pubTS, "")
	authorName := firstCleanText(statusEntry["uname"], data["uname"], data["name"], uid, roomID)
	statusLabel := "未开播"
	if status == 1 {
		statusLabel = "直播中"
	}
	return map[string]any{
		"id": "live-" + roomID, "service": "live", "category": "直播", "title": firstCleanText(data["title"], "直播间预览"),
		"summary": statusLabel, "url": canonicalURL, "pub_ts": pubTS, "created_at": startedAt,
		"author": map[string]any{"name": authorName, "avatar": normalizeBilibiliURL(firstNonNil(statusEntry["face"], data["face"])), "uid": uid},
		"images": images, "live_status": status, "live_event": map[bool]string{true: "started", false: "preview"}[status == 1],
		"status_label": statusLabel, "live_started_at": startedAt,
	}, nil
}

func sendBilibiliPreview(ctx context.Context, event *rayleabot.EventContext, update map[string]any, realPreview bool) error {
	item := subscription{
		ID:       "preview-bilibili-" + normalizedTargetType(event.Event.Target.Type) + "-" + firstText(event.Event.Target.ID, "current"),
		Platform: "bilibili", UID: "100000000000000009", Name: "RayleaBot 示例账号",
		TargetType: normalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Services: []string{"all"}, Subscribers: mergeSubscriber(nil, event), Enabled: true,
	}
	if realPreview {
		author := mapValue(update["author"])
		item.UID = firstText(author["uid"], update["id"], "preview")
		item.Name = firstText(author["name"], "Bilibili 预览")
	}
	data := buildBilibiliRenderData(item, update)
	inlineBilibiliUpdateAvatars(ctx, event.Actions(), data)
	imagePath, err := renderSubscriptionCardImage(ctx, event.Actions(), "bilibili-update", data, buildBilibiliFallback(data), previewRenderLogFields(update))
	if err != nil {
		return event.SendText(previewCardFailureText(err))
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func sampleBilibiliUpdate(service string) map[string]any {
	now := time.Now()
	base := map[string]any{
		"id": "preview-" + service, "service": service, "category": bilibiliServiceLabel(service),
		"author": map[string]any{"name": "RayleaBot 示例账号"}, "pub_ts": now.Unix(), "created_at": now.Format("2006-01-02 15:04"),
		"url":    "https://t.bilibili.com/100000000000000001",
		"images": []map[string]any{{"url": "https://i0.hdslb.com/bfs/archive/sample-cover.jpg"}},
	}
	switch service {
	case "live":
		base["id"], base["title"], base["summary"] = "preview-live", "直播间已开播", "直播中\n开播时间："+now.Format("2006-01-02 15:04")
		base["live_status"], base["live_event"], base["status_label"], base["live_started_at"] = 1, "started", "直播中", now.Format("2006-01-02 15:04")
		base["url"] = "https://live.bilibili.com/123456"
	case "image_text":
		base["title"], base["summary"] = "图文动态示例", "这里展示图文动态正文、图片九宫格、UP 主信息和订阅人身份。"
		base["images"] = []map[string]any{{"url": "https://i0.hdslb.com/bfs/new_dyn/sample-1.jpg"}, {"url": "https://i0.hdslb.com/bfs/new_dyn/sample-2.jpg"}, {"url": "https://i0.hdslb.com/bfs/new_dyn/sample-3.jpg"}}
	case "article":
		base["title"], base["summary"], base["url"] = "专栏文章示例", "文章摘要会显示在卡片正文区域，封面图显示在图片区域。", "https://www.bilibili.com/read/cv10001"
	case "repost":
		base["title"], base["summary"] = "转发动态示例", "转发评论会显示在主卡片正文中。"
		base["original"] = map[string]any{
			"id": "preview-original", "service": "video", "category": "视频", "title": "原动态视频标题",
			"summary": "原动态摘要会以内嵌卡片显示，包含原作者、正文、图片和链接。", "author": map[string]any{"name": "原动态作者"},
			"images": []map[string]any{{"url": "https://i0.hdslb.com/bfs/archive/original-cover.jpg"}},
			"url":    "https://www.bilibili.com/video/BV1RayleaBot", "created_at": now.Add(-5 * time.Minute).Format("2006-01-02 15:04"),
		}
	default:
		base["service"], base["category"], base["title"], base["summary"] = "video", "视频", "新视频示例", "视频简介会被整理为摘要，推送图会显示封面、时长、链接和订阅人。"
		base["duration_text"], base["url"] = "12:48", "https://www.bilibili.com/video/BV1RayleaBot"
	}
	return base
}

func cleanBilibiliTitle(value any) string {
	text := cleanText(value)
	return strings.TrimSuffix(text, " - 哔哩哔哩")
}

func htmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;").Replace(value)
}
