package plugin

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

const weiboFeedCollectMaxDepth = 8

var (
	weiboCropSizePattern  = regexp.MustCompile(`(?i)(/crop(?:\.\d+){4}\.)(\d+)(/)`)
	weiboOrjSizePattern   = regexp.MustCompile(`(?i)/(orj\d+|large|bmiddle|mw\d+|thumbnail)/`)
	weiboHTMLBreakPattern = regexp.MustCompile(`(?i)<br\s*/?>`)
	weiboHTMLBlockPattern = regexp.MustCompile(`(?i)</?(?:p|div|li)(?:\s[^>]*)?>`)
	weiboLongTextLink     = regexp.MustCompile(`(?is)>\s*全文\s*</a>\s*$`)
	weiboLongTextSuffix   = regexp.MustCompile(`(?:\.{3}|…)[[:space:]]*全文[[:space:]]*$`)
)

func weiboFeedUpdates(document map[string]any) []map[string]any {
	updates := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, mblog := range collectWeiboMblogs(document, 0) {
		update := normalizeWeiboMblog(mblog, 0)
		if update == nil {
			continue
		}
		id := stringScalar(update["id"])
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		updates = append(updates, update)
	}
	return updates
}

func collectWeiboMblogs(value any, depth int) []map[string]any {
	mblogs := make([]map[string]any, 0)
	collectWeiboMblogsInto(value, &mblogs, depth)
	return mblogs
}

func collectWeiboMblogsInto(value any, mblogs *[]map[string]any, depth int) {
	if depth > weiboFeedCollectMaxDepth || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if mblog := mapValue(typed["mblog"]); mblog != nil && !weiboMblogIsAd(mblog) && !weiboMblogIsAd(typed) {
			*mblogs = append(*mblogs, mblog)
		}
		for _, key := range []string{"data", "cards", "card_group", "cardlistInfo", "mblog"} {
			if key == "mblog" {
				continue
			}
			if nested, exists := typed[key]; exists {
				collectWeiboMblogsInto(nested, mblogs, depth+1)
			}
		}
	case []any:
		for _, item := range typed {
			collectWeiboMblogsInto(item, mblogs, depth+1)
		}
	}
}

func weiboMblogIsAd(object map[string]any) bool {
	if object == nil {
		return false
	}
	if boolScalar(object["is_ad"]) || boolScalar(object["isAd"]) {
		return true
	}
	return mapValue(object["promotion"]) != nil
}

func normalizeWeiboMblog(mblog map[string]any, depth int) map[string]any {
	if mblog == nil || weiboMblogIsAd(mblog) {
		return nil
	}
	id := firstText(mblog["mid"], mblog["idstr"], mblog["id"], mblog["bid"])
	author := mapValue(mblog["user"])
	uid := firstText(author["id"], author["idstr"], author["uid"])
	name := cleanWeiboSearchText(firstText(author["screen_name"], author["name"], author["nickname"]))
	summary, needsLongText := weiboMblogSummary(mblog)
	service := weiboMblogService(mblog)
	title := weiboDistinctTitle(nestedValue(mblog, "page_info", "page_title"), summary)
	pubTS := weiboPubTS(mblog)
	if id == "" || uid == "" || pubTS <= 0 {
		return nil
	}
	var original map[string]any
	if service == "repost" && depth < 2 {
		original = normalizeWeiboMblog(mapValue(mblog["retweeted_status"]), depth+1)
		if summary == "" {
			summary = "转发微博"
		}
	}
	images := weiboMblogImages(mblog, service)
	return map[string]any{
		"id": id, "platform": "weibo", "uid": uid, "service": service,
		"category": weiboServiceCategory(service), "title": title,
		"summary": summary, "summary_html": "",
		"needs_long_text": needsLongText,
		"url":             weiboStatusURL(uid, mblog, id), "pub_ts": pubTS,
		"created_at":    formatBilibiliTime(pubTS, stringScalar(mblog["created_at"])),
		"duration_text": weiboMblogDuration(mblog),
		"author": map[string]any{
			"name": firstText(name, uid), "uid": uid,
			"avatar": weiboAuthorAvatar(author),
		},
		"images": images, "original": original,
	}
}

func weiboMblogSummary(mblog map[string]any) (string, bool) {
	fullText := firstText(
		nestedValue(mblog, "longText", "longTextContent"),
		nestedValue(mblog, "long_text", "long_text_content"),
		mblog["longTextContent"],
		mblog["long_text_content"],
	)
	if summary := weiboPlainText(fullText); summary != "" {
		return summary, false
	}
	rawText := stringScalar(mblog["text"])
	summary := weiboPlainText(rawText)
	needsLongText := boolScalar(mblog["isLongText"]) || boolScalar(mblog["is_long_text"]) || weiboLongTextLink.MatchString(rawText) || weiboLongTextSuffix.MatchString(summary)
	return summary, needsLongText
}

func weiboIncompleteSummary(summary string) string {
	summary = strings.TrimSpace(weiboLongTextSuffix.ReplaceAllString(strings.TrimSpace(summary), ""))
	const notice = "正文获取不完整，请查看原微博链接。"
	if summary == "" {
		return notice
	}
	return summary + "\n\n" + notice
}

func weiboMblogService(mblog map[string]any) string {
	if mapValue(mblog["retweeted_status"]) != nil {
		return "repost"
	}
	if weiboMblogHasVideo(mblog) {
		return "video"
	}
	if len(weiboMblogImages(mblog, "image")) > 0 {
		return "image"
	}
	return "post"
}

func weiboServiceCategory(service string) string {
	switch service {
	case "image":
		return "图片微博"
	case "video":
		return "视频微博"
	case "repost":
		return "转发微博"
	default:
		return "文字微博"
	}
}

func weiboMblogHasVideo(mblog map[string]any) bool {
	page := mapValue(mblog["page_info"])
	if page == nil {
		return false
	}
	pageType := strings.ToLower(firstText(page["type"], page["object_type"]))
	if strings.Contains(pageType, "video") {
		return true
	}
	return mapValue(page["media_info"]) != nil
}

func weiboMblogImages(mblog map[string]any, service string) []map[string]any {
	images := make([]map[string]any, 0)
	for _, raw := range sliceValue(mblog["pics"]) {
		pic := mapValue(raw)
		imageURL := firstWeiboImageURL(
			pic["url"], nestedValue(pic, "large", "url"), nestedValue(pic, "geo", "url"),
		)
		if imageURL == "" {
			continue
		}
		images = append(images, map[string]any{
			"url": imageURL, "width": intScalar(firstNonNil(nestedValue(pic, "large", "geo", "width"), nestedValue(pic, "geo", "width"))),
			"height": intScalar(firstNonNil(nestedValue(pic, "large", "geo", "height"), nestedValue(pic, "geo", "height"))),
		})
		if len(images) == 9 {
			return images
		}
	}
	if len(images) > 0 {
		return images
	}
	if service != "video" && !weiboMblogHasVideo(mblog) {
		return images
	}
	cover := weiboAllowedImageURL(normalizeBilibiliURL(firstNonNil(
		nestedValue(mblog, "page_info", "page_pic", "url"),
		nestedValue(mblog, "page_info", "page_pic"),
	)))
	if cover != "" {
		images = append(images, map[string]any{"url": cover})
	}
	return images
}

func firstWeiboImageURL(values ...any) string {
	for _, value := range values {
		if imageURL := weiboAllowedImageURL(normalizeBilibiliURL(value)); imageURL != "" {
			return imageURL
		}
	}
	return ""
}

func weiboMblogDuration(mblog map[string]any) string {
	page := mapValue(mblog["page_info"])
	if page == nil {
		return ""
	}
	media := mapValue(page["media_info"])
	for _, candidate := range []any{
		media["duration_player"], media["duration"], media["video_duration"], media["duration_time"],
		page["duration"], page["video_duration"],
	} {
		if text := weiboVideoDurationText(candidate); text != "" {
			return text
		}
	}
	return ""
}

func weiboVideoDurationText(value any) string {
	text := strings.TrimSpace(stringScalar(value))
	if strings.Contains(text, ":") {
		return text
	}
	seconds := int64(floatScalar(value))
	if seconds <= 0 {
		return ""
	}
	return formatVideoDuration(seconds)
}

func weiboStatusURL(uid string, mblog map[string]any, id string) string {
	if bid := strings.TrimSpace(stringScalar(mblog["bid"])); bid != "" && uid != "" {
		return "https://weibo.com/" + uid + "/" + bid
	}
	if id != "" {
		return "https://m.weibo.cn/status/" + id
	}
	return ""
}

func weiboPubTS(mblog map[string]any) int64 {
	if ts := intScalar(mblog["created_timestamp"]); ts > 0 {
		return ts
	}
	if ts := intScalar(mblog["created_at"]); ts > 1_000_000_000 {
		return ts
	}
	text := strings.TrimSpace(stringScalar(mblog["created_at"]))
	if text == "" {
		return 0
	}
	for _, layout := range []string{
		time.RubyDate,
		"Mon Jan 02 15:04:05 -0700 2006",
		"2006-01-02 15:04:05",
		time.RFC1123Z,
	} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}

func weiboNormalizeSkipReason(mblog map[string]any) string {
	if mblog == nil {
		return "接口没有返回微博正文"
	}
	if weiboMblogIsAd(mblog) {
		return "这条微博被识别为广告"
	}
	if firstText(mblog["mid"], mblog["idstr"], mblog["id"], mblog["bid"]) == "" {
		return "缺少微博 ID"
	}
	author := mapValue(mblog["user"])
	if firstText(author["id"], author["idstr"], author["uid"]) == "" {
		return "缺少博主 UID"
	}
	if weiboPubTS(mblog) <= 0 {
		return "缺少或无法解析发布时间"
	}
	return "微博字段不完整"
}

func weiboAuthorAvatar(author map[string]any) string {
	return weiboCardAvatarURL(firstNonNil(author["profile_image_url"], author["avatar_large"], author["avatar"], author["avatar_hd"]))
}

func weiboCardAvatarURL(raw any) string {
	allowed := weiboAllowedImageURL(normalizeBilibiliURL(raw))
	if allowed == "" {
		return ""
	}
	parsed, err := url.Parse(allowed)
	if err != nil {
		return allowed
	}
	path := parsed.Path
	if updated := weiboCropSizePattern.ReplaceAllString(path, "${1}180${3}"); updated != path {
		parsed.Path = updated
		parsed.RawPath = ""
		return parsed.String()
	}
	if weiboOrjSizePattern.MatchString(path) {
		parsed.Path = weiboOrjSizePattern.ReplaceAllString(path, "/orj180/")
		parsed.RawPath = ""
		return parsed.String()
	}
	return parsed.String()
}

func weiboPlainText(value any) string {
	text := weiboHTMLBreakPattern.ReplaceAllString(stringScalar(value), "\n")
	text = weiboHTMLBlockPattern.ReplaceAllString(text, "\n")
	text = weiboHTMLTagPattern.ReplaceAllString(text, "")
	return cleanText(text)
}

func weiboDistinctTitle(value any, summary string) string {
	title := truncateRunes(cleanText(value), 72)
	if title == "" || title == truncateRunes(cleanText(summary), 72) {
		return ""
	}
	return title
}

func weiboAllowedImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, _, err := validateAvatarSourceURL(raw); err != nil {
		return ""
	}
	return raw
}
