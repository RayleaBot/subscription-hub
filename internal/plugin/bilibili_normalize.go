package plugin

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	bilibiliTopicPattern = regexp.MustCompile(`#[^#\r\n]+#`)
	bilibiliBVIDPattern  = regexp.MustCompile(`^BV[0-9A-Za-z]+$`)
	bilibiliClassPattern = regexp.MustCompile(`[^0-9A-Za-z_-]+`)
)

func dynamicUpdates(document map[string]any) []map[string]any {
	data := mapValue(document["data"])
	if data == nil {
		return nil
	}
	items := append([]any(nil), sliceValue(data["items"])...)
	items = append(items, sliceValue(data["cards"])...)
	updates := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if update := normalizeDynamicItem(mapValue(raw), 0); update != nil {
			updates = append(updates, update)
		}
	}
	return updates
}

func normalizeDynamicItem(item map[string]any, depth int) map[string]any {
	if item == nil {
		return nil
	}
	basic := mapValue(item["basic"])
	modules := mapValue(item["modules"])
	authorModule := mapValue(modules["module_author"])
	dynamicModule := mapValue(modules["module_dynamic"])
	tagModule := mapValue(modules["module_tag"])
	description := mapValue(dynamicModule["desc"])
	major := mapValue(dynamicModule["major"])

	dynamicID := firstText(item["id_str"], item["id"])
	itemType := stringScalar(item["type"])
	service := dynamicService(itemType, basic, major)
	authorName := stringScalar(authorModule["name"])
	title := dynamicTitle(major, description, service, itemType, authorName)
	summary := dynamicSummary(description, major)
	topic := dynamicTopic(item, basic, major)
	topicText := ""
	if topic != nil {
		topicText = "#" + stringScalar(topic["name"]) + "#"
	}
	summaryHTML := dynamicSummaryHTML(description, major, summary, topicText)
	pubTS := intScalar(authorModule["pub_ts"])
	if dynamicID == "" || title == "" || pubTS <= 0 {
		return nil
	}
	original := map[string]any(nil)
	if itemType == "DYNAMIC_TYPE_FORWARD" && depth < 2 {
		original = normalizeDynamicItem(mapValue(item["orig"]), depth+1)
	}
	if service == "repost" && original != nil {
		if summary == "" {
			summary = "转发动态"
		}
		if summaryHTML == "" {
			summaryHTML = "转发动态"
		}
	}
	author := map[string]any{
		"name":   authorName,
		"avatar": normalizeBilibiliURL(authorModule["face"]),
		"uid":    cleanText(authorModule["mid"]),
	}
	return map[string]any{
		"id": dynamicID, "type": itemType, "service": service,
		"category": dynamicCategory(service), "title": title,
		"summary": summary, "summary_html": summaryHTML,
		"url":    dynamicJumpURL(basic, major, dynamicID),
		"pub_ts": pubTS, "created_at": formatBilibiliTime(pubTS, stringScalar(authorModule["pub_time"])),
		"duration_text": dynamicDuration(major, service),
		"author":        author, "images": dynamicImages(major, service),
		"topic": topic, "is_pinned": cleanText(tagModule["text"]) == "置顶",
		"original": original,
	}
}

func dynamicService(itemType string, basic, major map[string]any) string {
	majorType := strings.ToUpper(stringScalar(major["type"]))
	commentType := stringScalar(basic["comment_type"])
	switch {
	case itemType == "DYNAMIC_TYPE_FORWARD" || commentType == "17":
		return "repost"
	case itemType == "DYNAMIC_TYPE_AV" || majorType == "MAJOR_TYPE_ARCHIVE":
		return "video"
	case itemType == "DYNAMIC_TYPE_ARTICLE" || majorType == "MAJOR_TYPE_ARTICLE":
		return "article"
	case itemType == "DYNAMIC_TYPE_DRAW" || itemType == "DYNAMIC_TYPE_WORD":
		return "image_text"
	case majorType == "MAJOR_TYPE_DRAW" || majorType == "MAJOR_TYPE_OPUS" || majorType == "MAJOR_TYPE_PGC" || majorType == "MAJOR_TYPE_COMMON":
		return "image_text"
	default:
		return "image_text"
	}
}

func dynamicCategory(service string) string {
	switch service {
	case "video":
		return "视频动态"
	case "image_text":
		return "图文动态"
	case "article":
		return "文章动态"
	case "repost":
		return "转发动态"
	default:
		return "动态"
	}
}

func dynamicTitle(major, description map[string]any, service, itemType, authorName string) string {
	if service == "repost" {
		return "转发动态"
	}
	for _, sectionName := range []string{"archive", "article", "opus", "common"} {
		if title := cleanText(nestedValue(major, sectionName, "title")); title != "" {
			return title
		}
	}
	action := "发布新内容"
	switch {
	case service == "video":
		action = "发布新视频"
	case service == "article":
		action = "发布新文章"
	case service == "repost":
		action = "转发动态"
	case itemType == "DYNAMIC_TYPE_WORD":
		action = "发布文字动态"
	case service == "image_text":
		action = "发布图文动态"
	}
	if authorName != "" {
		return authorName + " " + action
	}
	return action
}

func dynamicSummary(description, major map[string]any) string {
	if description != nil {
		if text := cleanText(description["rich_text_nodes"]); text != "" {
			return truncateRunes(text, 420)
		}
		if text := cleanText(description["text"]); text != "" {
			return truncateRunes(text, 420)
		}
	}
	for _, sectionName := range []string{"archive", "article", "opus", "draw", "common"} {
		section := mapValue(major[sectionName])
		if section == nil {
			continue
		}
		if text := firstCleanText(section["desc"], section["summary"], section["content"]); text != "" {
			return truncateRunes(text, 420)
		}
	}
	return ""
}

func dynamicSummaryHTML(description, major map[string]any, summary, topic string) string {
	if description != nil {
		if nodes := sliceValue(description["rich_text_nodes"]); len(nodes) > 0 {
			if rendered := richTextNodesHTML(nodes); rendered != "" {
				return withStandaloneTopic(rendered, summary, topic)
			}
		}
	}
	if rendered := richTextFromMajor(major); rendered != "" {
		return withStandaloneTopic(rendered, summary, topic)
	}
	if text := cleanText(description["text"]); text != "" {
		return withStandaloneTopic(richTextFallbackHTML(text), summary, topic)
	}
	if summary != "" {
		return withStandaloneTopic(richTextFallbackHTML(summary), summary, topic)
	}
	return ""
}

func dynamicTopic(item, basic, major map[string]any) map[string]any {
	paths := [][]any{
		{"modules", "module_dynamic", "topic"}, {"basic", "topic"}, {"topic"}, {"opus", "topic"},
	}
	for _, path := range paths {
		root := any(item)
		if path[0] == "basic" {
			root, path = basic, path[1:]
		} else if path[0] == "opus" {
			root = major
		}
		if topic := topicFromValue(nestedValue(root, path...)); topic != nil {
			return topic
		}
	}
	for _, value := range []any{
		nestedValue(item, "modules", "module_dynamic", "topic", "name"),
		nestedValue(item, "modules", "module_dynamic", "topic", "title"),
		nestedValue(item, "modules", "module_dynamic", "topic", "text"),
		nestedValue(basic, "topic", "name"), nestedValue(basic, "topic", "title"),
		nestedValue(item, "topic", "name"), nestedValue(major, "opus", "topic", "name"),
	} {
		if topic := topicFromValue(value); topic != nil {
			return topic
		}
	}
	return nil
}

func topicFromValue(value any) map[string]any {
	if object := mapValue(value); object != nil {
		name := strings.Trim(cleanText(firstNonNil(object["name"], object["title"], object["text"])), "# \t\r\n")
		if name == "" {
			return nil
		}
		topic := map[string]any{"name": name}
		if id := intScalar(object["id"]); id > 0 {
			topic["id"] = id
		}
		if jumpURL := normalizeBilibiliURL(object["jump_url"]); jumpURL != "" {
			topic["jump_url"] = jumpURL
		}
		return topic
	}
	name := strings.Trim(cleanText(value), "# \t\r\n")
	if name == "" {
		return nil
	}
	return map[string]any{"name": name}
}

func richTextFromMajor(major map[string]any) string {
	for _, sectionName := range []string{"archive", "article", "opus", "draw", "common"} {
		section := mapValue(major[sectionName])
		if section == nil {
			continue
		}
		for _, key := range []string{"summary", "content", "desc", "paragraphs"} {
			if rendered := richTextHTML(section[key]); rendered != "" {
				return rendered
			}
		}
	}
	return ""
}

func richTextHTML(value any) string {
	if object := mapValue(value); object != nil {
		if nodes := sliceValue(object["rich_text_nodes"]); len(nodes) > 0 {
			if rendered := richTextNodesHTML(nodes); rendered != "" {
				return rendered
			}
		}
		for _, key := range []string{"paragraphs", "text", "orig_text", "title", "desc", "summary", "content"} {
			if rendered := richTextHTML(object[key]); rendered != "" {
				return rendered
			}
		}
		return ""
	}
	if values := sliceValue(value); len(values) > 0 {
		parts := make([]string, 0, len(values))
		for _, item := range values {
			if rendered := richTextHTML(item); rendered != "" {
				parts = append(parts, rendered)
			}
		}
		return strings.Join(parts, "<br>")
	}
	if text := rawBilibiliText(value); strings.TrimSpace(text) != "" {
		return richTextFallbackHTML(text)
	}
	return ""
}

func richTextFallbackHTML(value string) string {
	value = rawBilibiliText(value)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	indices := bilibiliTopicPattern.FindAllStringIndex(value, -1)
	if len(indices) == 0 {
		return strings.ReplaceAll(html.EscapeString(value), "\n", "<br>")
	}
	var builder strings.Builder
	offset := 0
	for _, index := range indices {
		builder.WriteString(strings.ReplaceAll(html.EscapeString(value[offset:index[0]]), "\n", "<br>"))
		topic := value[index[0]:index[1]]
		builder.WriteString(`<span class="rich-text-topic bili-rich-text-module topic">`)
		builder.WriteString(html.EscapeString(topic))
		builder.WriteString(`</span>`)
		offset = index[1]
	}
	builder.WriteString(strings.ReplaceAll(html.EscapeString(value[offset:]), "\n", "<br>"))
	return builder.String()
}

func richTextNodesHTML(nodes []any) string {
	var builder strings.Builder
	for _, raw := range nodes {
		builder.WriteString(richTextNodeHTML(mapValue(raw), false))
	}
	return builder.String()
}

func richTextNodeHTML(node map[string]any, classifyMissing bool) string {
	if node == nil {
		return ""
	}
	nodeType := stringScalar(node["type"])
	text := rawBilibiliText(firstNonNil(node["text"], node["orig_text"]))
	if richTextEmojiURL(node) != "" && (nodeType == "" || nodeType == "RICH_TEXT_NODE_TYPE_TEXT") {
		nodeType = "RICH_TEXT_NODE_TYPE_EMOJI"
	}
	if classifyMissing && nodeType == "" {
		nodeType = classifyRichText(strings.TrimSpace(text), node)
	}
	if nodeType == "" {
		nodeType = classifyRichText(strings.TrimSpace(text), node)
	}
	escaped := html.EscapeString(text)
	switch nodeType {
	case "RICH_TEXT_NODE_TYPE_TEXT":
		return strings.ReplaceAll(escaped, "\n", "<br>")
	case "RICH_TEXT_NODE_TYPE_TOPIC":
		return `<span class="rich-text-topic bili-rich-text-module topic">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_AT":
		return `<span class="rich-text-at bili-rich-text-module at">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_LOTTERY":
		return `<span class="rich-text-lottery bili-rich-text-module lottery">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_BV":
		return `<span class="rich-text-link bili-rich-text-link video">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_EMOJI":
		return richTextEmojiHTML(node, text)
	case "RICH_TEXT_NODE_TYPE_VOTE":
		return `<span class="rich-text-link bili-rich-text-module vote">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_GOODS":
		className := "rich-text-link bili-rich-text-module goods"
		if token := bilibiliClassPattern.ReplaceAllString(stringScalar(node["icon_name"]), ""); token != "" {
			className += " " + token
		}
		return `<span class="` + className + `">` + escaped + `</span>`
	case "RICH_TEXT_NODE_TYPE_WEB":
		classified := classifyRichText(strings.TrimSpace(text), node)
		if classified != "" && classified != "RICH_TEXT_NODE_TYPE_TEXT" && classified != nodeType {
			copy := cloneJSONMap(node)
			copy["type"] = classified
			return richTextNodeHTML(copy, false)
		}
		if text != "" {
			return `<span class="rich-text-link bili-rich-text-link web">` + escaped + `</span>`
		}
		return ""
	default:
		classified := classifyRichText(strings.TrimSpace(text), node)
		if classified != "" && classified != "RICH_TEXT_NODE_TYPE_TEXT" && classified != nodeType {
			copy := cloneJSONMap(node)
			copy["type"] = classified
			return richTextNodeHTML(copy, false)
		}
		return escaped
	}
}

func classifyRichText(text string, node map[string]any) string {
	if richTextEmojiURL(node) != "" {
		return "RICH_TEXT_NODE_TYPE_EMOJI"
	}
	if bilibiliTopicPattern.MatchString(text) && bilibiliTopicPattern.FindString(text) == text {
		return "RICH_TEXT_NODE_TYPE_TOPIC"
	}
	if strings.HasPrefix(text, "@") {
		return "RICH_TEXT_NODE_TYPE_AT"
	}
	if text == "互动抽奖" {
		return "RICH_TEXT_NODE_TYPE_LOTTERY"
	}
	if bilibiliBVIDPattern.MatchString(text) {
		return "RICH_TEXT_NODE_TYPE_BV"
	}
	iconName := strings.ToLower(stringScalar(node["icon_name"]))
	jumpURL := strings.ToLower(firstText(node["jump_url"], node["url"]))
	if strings.Contains(iconName, "vote") || strings.Contains(jumpURL, "vote") {
		return "RICH_TEXT_NODE_TYPE_VOTE"
	}
	if strings.Contains(iconName, "taobao") || strings.Contains(iconName, "goods") || strings.Contains(jumpURL, "mall") {
		return "RICH_TEXT_NODE_TYPE_GOODS"
	}
	return "RICH_TEXT_NODE_TYPE_TEXT"
}

func richTextEmojiURL(node map[string]any) string {
	emoji := mapValue(node["emoji"])
	for _, value := range []any{
		emoji["icon_url"], emoji["url"], emoji["image_url"], emoji["gif_url"], emoji["webp_url"],
		node["icon_url"], node["url"], node["image_url"],
	} {
		if normalized := normalizeBilibiliURL(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func richTextEmojiHTML(node map[string]any, nodeText string) string {
	emoji := mapValue(node["emoji"])
	imageURL := richTextEmojiURL(node)
	if imageURL == "" {
		return html.EscapeString(nodeText)
	}
	emojiText := firstText(emoji["text"], nodeText)
	size := floatScalar(emoji["size"])
	if size <= 0 {
		size = 1
	}
	sizeCSS := strconv.FormatFloat(size*1.5, 'f', 2, 64) + "em"
	return `<img class="rich-text-emoji" src="` + html.EscapeString(imageURL) + `" alt="` + html.EscapeString(emojiText) + `" title="` + html.EscapeString(emojiText) + `" style="width:` + sizeCSS + `;height:` + sizeCSS + `;">`
}

func withStandaloneTopic(rendered, summary, topic string) string {
	topic = strings.TrimSpace(topic)
	name := strings.Trim(topic, "# \t\r\n")
	if topic == "" || strings.Contains(summary, topic) || (name != "" && strings.Contains(summary, name)) || strings.Contains(rendered, html.EscapeString(topic)) {
		return rendered
	}
	prefix := `<span class="rich-text-topic bili-rich-text-module topic">` + html.EscapeString(topic) + `</span>`
	if strings.TrimSpace(rendered) == "" {
		return prefix
	}
	return prefix + "<br>" + rendered
}

func dynamicDuration(major map[string]any, service string) string {
	if service != "video" {
		return ""
	}
	archive := mapValue(major["archive"])
	if value := cleanText(archive["duration_text"]); value != "" {
		return value
	}
	if value := cleanText(archive["duration"]); strings.Contains(value, ":") {
		return value
	}
	return formatVideoDuration(intScalar(archive["duration"]))
}

func dynamicJumpURL(basic, major map[string]any, dynamicID string) string {
	if value := normalizeBilibiliURL(basic["jump_url"]); value != "" {
		return value
	}
	for _, sectionName := range []string{"archive", "article", "opus", "common"} {
		if value := normalizeBilibiliURL(nestedValue(major, sectionName, "jump_url")); value != "" {
			return value
		}
	}
	return "https://t.bilibili.com/" + dynamicID
}

func dynamicImages(major map[string]any, service string) []map[string]any {
	images := make([]map[string]any, 0, 9)
	switch service {
	case "video":
		appendBilibiliImage(&images, nestedValue(major, "archive", "cover"))
	case "article":
		covers := nestedValue(major, "article", "covers")
		if values := sliceValue(covers); len(values) > 0 {
			for _, value := range values {
				appendBilibiliImage(&images, value)
			}
		} else {
			appendBilibiliImage(&images, covers)
		}
		for _, value := range sliceValue(nestedValue(major, "opus", "pics")) {
			appendBilibiliImage(&images, value)
		}
	default:
		for _, value := range sliceValue(nestedValue(major, "draw", "items")) {
			appendBilibiliImage(&images, value)
		}
		for _, value := range sliceValue(nestedValue(major, "opus", "pics")) {
			appendBilibiliImage(&images, value)
		}
		appendBilibiliImage(&images, nestedValue(major, "common", "cover"))
	}
	if len(images) > 9 {
		images = images[:9]
	}
	return images
}

func appendBilibiliImage(images *[]map[string]any, value any) {
	if text, ok := value.(string); ok {
		if normalized := normalizeBilibiliURL(text); normalized != "" {
			*images = append(*images, map[string]any{"url": normalized})
		}
		return
	}
	object := mapValue(value)
	if object == nil {
		return
	}
	imageURL := normalizeBilibiliURL(firstNonNil(object["url"], object["src"], object["cover"]))
	if imageURL == "" {
		return
	}
	image := map[string]any{"url": imageURL}
	for _, key := range []string{"width", "height"} {
		if number := intScalar(object[key]); number > 0 {
			image[key] = number
		}
	}
	*images = append(*images, image)
}

func liveUpdate(document map[string]any, uid string) map[string]any {
	entry := mapValue(nestedValue(document, "data", uid))
	if entry == nil {
		return nil
	}
	status := intScalar(entry["live_status"])
	roomID := stringScalar(entry["room_id"])
	title := cleanText(entry["title"])
	if title == "" {
		title = "直播间状态更新"
	}
	pubTS := liveStartTimestamp(entry)
	startedAt := formatBilibiliTime(pubTS, "")
	statusLabel := "已下播"
	if status == 1 {
		statusLabel = "直播中"
	}
	summary := statusLabel
	if startedAt != "" {
		summary += "\n开播时间：" + startedAt
	}
	sessionID := stringScalar(pubTS)
	if sessionID == "0" || sessionID == "" {
		sessionID = firstText(entry["live_id"], entry["session_id"])
	}
	idParts := []string{"live", uid, strconv.FormatInt(status, 10), roomID}
	if sessionID != "" {
		idParts = append(idParts, sessionID)
	}
	return map[string]any{
		"id": strings.Join(idParts, "-"), "service": "live", "category": "直播",
		"title": title, "summary": summary, "url": liveRoomLink(entry, roomID),
		"pub_ts": pubTS, "created_at": startedAt,
		"author": map[string]any{"name": firstCleanText(entry["uname"], uid), "avatar": normalizeBilibiliURL(entry["face"]), "uid": uid},
		"images": liveCoverImages(entry), "live_status": status,
		"live_event":   map[bool]string{true: "started", false: "ended"}[status == 1],
		"status_label": statusLabel, "live_started_at": startedAt, "room_id": roomID,
	}
}

func liveStartTimestamp(entry map[string]any) int64 {
	for _, key := range []string{"liveTime", "live_time", "live_start_time"} {
		if number := intScalar(entry[key]); number > 0 {
			return number
		}
		text := stringScalar(entry[key])
		if text == "" || strings.HasPrefix(text, "0000-00-00") {
			continue
		}
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
			if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
				return parsed.Unix()
			}
		}
	}
	return 0
}

func liveCoverImages(entry map[string]any) []map[string]any {
	images := make([]map[string]any, 0, 1)
	for _, key := range []string{"cover_from_user", "user_cover", "cover", "keyframe"} {
		appendBilibiliImage(&images, entry[key])
		if len(images) > 0 {
			break
		}
	}
	return images
}

func liveRoomLink(entry map[string]any, roomID string) string {
	if value := normalizeBilibiliURL(firstNonNil(entry["url"], entry["link"])); value != "" {
		return value
	}
	if roomID == "" {
		return ""
	}
	return "https://live.bilibili.com/" + roomID
}

func cleanText(value any) string {
	if value == nil {
		return ""
	}
	if object := mapValue(value); object != nil {
		for _, key := range []string{"text", "orig_text", "title", "desc", "summary", "content"} {
			if text := cleanText(object[key]); text != "" {
				return text
			}
		}
		for _, key := range []string{"rich_text_nodes", "paragraphs"} {
			if text := cleanText(object[key]); text != "" {
				return text
			}
		}
		return ""
	}
	if values := sliceValue(value); len(values) > 0 {
		parts := make([]string, 0, len(values))
		for _, item := range values {
			if text := cleanText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	text := rawBilibiliText(value)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func rawBilibiliText(value any) string {
	text := stringScalar(value)
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\\r\\n", "\n")
	text = strings.ReplaceAll(text, "\\n", "\n")
	text = strings.ReplaceAll(text, "\\t", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func normalizeBilibiliURL(value any) string {
	text := strings.TrimSpace(stringScalar(value))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "//") {
		return "https:" + text
	}
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return text
	}
	return ""
}

func formatVideoDuration(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remaining := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remaining)
	}
	return fmt.Sprintf("%d:%02d", minutes, remaining)
}

func formatBilibiliTime(timestamp int64, fallback string) string {
	if timestamp > 0 {
		return time.Unix(timestamp, 0).Local().Format("2006年01月02日 15:04")
	}
	return strings.TrimSpace(fallback)
}

func truncateRunes(value string, limit int) string {
	value = cleanText(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func firstText(values ...any) string {
	for _, value := range values {
		if text := stringScalar(value); text != "" {
			return text
		}
	}
	return ""
}

func firstCleanText(values ...any) string {
	for _, value := range values {
		if text := cleanText(value); text != "" {
			return text
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil && stringScalar(value) != "" {
			return value
		}
		if mapValue(value) != nil || sliceValue(value) != nil {
			return value
		}
	}
	return nil
}
