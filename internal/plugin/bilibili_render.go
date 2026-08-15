package plugin

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

var bilibiliServiceLabels = map[string]string{
	"live": "直播", "video": "视频", "image_text": "图文", "article": "文章", "repost": "转发", "all": "全部",
}

func buildBilibiliRenderData(item subscription, update map[string]any) map[string]any {
	subscriberCards := buildSubscriberCards(item.Subscribers)
	author := renderSubscriptionAuthor(item, update["author"])
	title := truncateRunes(firstText(update["title"], "订阅更新"), 72)
	summary := truncateRunes(stringScalar(update["summary"]), 420)
	summaryHTML := limitBilibiliHTML(stringScalar(update["summary_html"]), 420)
	service := bilibiliServiceLabel(stringScalar(update["service"]))
	category := firstText(update["category"], service)
	images := imageMaps(update["images"], 9)
	mediaItems := buildMediaItems(images, stringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": summaryHTML,
		"subtitle": "Bilibili · " + service, "source_label": "Bilibili · " + category,
		"platform": "Bilibili", "service": service, "category": category,
		"author": author, "author_uid_text": uidText(firstText(author["uid"], item.UID)),
		"summary": summary, "summary_html": summaryHTML, "topic": renderTopic(update["topic"]),
		"images": images, "image_count": len(mediaItems), "media_grid_class": mediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": stringScalar(update["duration_text"]), "url": stringScalar(update["url"]),
		"pub_ts": intScalar(update["pub_ts"]), "created_at": stringScalar(update["created_at"]),
		"live_event": stringScalar(update["live_event"]), "live_status": update["live_status"],
		"status_label": stringScalar(update["status_label"]), "live_started_at": stringScalar(update["live_started_at"]),
		"live_detected_at": stringScalar(update["live_detected_at"]),
		"original":         renderOriginal(update["original"]),
		"subscription":     map[string]any{"uid": item.UID, "name": firstText(item.Name, item.UID)},
		"subscribers":      item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": subscriberNames(subscriberCards),
	}
}

func renderSubscriptionAuthor(item subscription, value any) map[string]any {
	author := cloneJSONMap(mapValue(value))
	if author == nil {
		author = map[string]any{}
	}
	author["name"] = firstText(author["name"], item.Name, item.UID)
	author["uid"] = firstText(author["uid"], item.UID)
	author["avatar"] = firstText(author["avatar"], item.AvatarURL)
	return author
}

func buildBilibiliFallback(data map[string]any) string {
	lines := []string{
		firstText(data["source_label"], data["subtitle"], "Bilibili 订阅更新"),
		firstText(data["headline"], data["title"]),
		firstText(data["content_text"], data["summary"]),
		originalFallback(data["original"]),
	}
	if subscribers := stringScalar(data["subscriber_text"]); subscribers != "" {
		lines = append(lines, "订阅人："+subscribers)
	}
	lines = append(lines, stringScalar(data["url"]))
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return strings.Join(result, "\n")
}

func renderOriginal(value any) map[string]any {
	original := mapValue(value)
	if original == nil {
		return nil
	}
	service := bilibiliServiceLabel(stringScalar(original["service"]))
	images := imageMaps(original["images"], 6)
	mediaItems := buildMediaItems(images, stringScalar(original["duration_text"]), service)
	author := mapValue(original["author"])
	return map[string]any{
		"title":   truncateRunes(firstText(original["title"], "原动态"), 72),
		"service": service, "category": firstText(original["category"], service), "author": author,
		"author_uid_text": uidText(stringScalar(author["uid"])),
		"summary":         truncateRunes(stringScalar(original["summary"]), 260), "summary_html": limitBilibiliHTML(stringScalar(original["summary_html"]), 260),
		"topic": renderTopic(original["topic"]), "images": imageMaps(images, 3),
		"image_count": len(mediaItems), "media_grid_class": mediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": stringScalar(original["duration_text"]), "url": stringScalar(original["url"]), "created_at": stringScalar(original["created_at"]),
	}
}

func originalFallback(value any) string {
	original := mapValue(value)
	if original == nil {
		return ""
	}
	parts := []string{"原动态", stringScalar(original["title"]), stringScalar(original["summary"])}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return strings.Join(result, "\n")
}

func renderTopic(value any) map[string]any {
	topic := topicFromValue(value)
	if topic == nil {
		return nil
	}
	name := stringScalar(topic["name"])
	return map[string]any{"name": name, "label": "# " + name, "url": stringScalar(topic["jump_url"])}
}

func buildSubscriberCards(items []subscriber) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		nickname := firstText(item.Nickname, id)
		displayName := firstText(item.GroupNickname, nickname)
		roleLabel := firstText(item.RoleLabel, subscriberRoleLabel(item.Role))
		avatarURL := firstText(item.AvatarURL, qqAvatarURL(id))
		result = append(result, map[string]any{
			"id": id, "nickname": nickname, "group_nickname": item.GroupNickname, "display_name": displayName,
			"title": item.Title, "role": item.Role, "role_label": roleLabel, "avatar_url": avatarURL, "uid_text": id,
		})
	}
	return result
}

func subscriberNames(cards []map[string]any) string {
	names := make([]string, 0, len(cards))
	for _, card := range cards {
		if name := firstText(card["display_name"], card["nickname"], card["id"]); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

func subscriberRoleLabel(role string) string {
	switch role {
	case "super_admin":
		return "超级管理员"
	case "owner":
		return "群主"
	case "admin":
		return "管理员"
	case "member":
		return "群员"
	default:
		return ""
	}
}

func qqAvatarURL(id string) string {
	if digits(id) == "" {
		return ""
	}
	return "https://q1.qlogo.cn/g?b=qq&nk=" + id + "&s=100"
}

func uidText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "UID " + value
}

func imageMaps(value any, limit int) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		if len(typed) > limit {
			typed = typed[:limit]
		}
		result := make([]map[string]any, len(typed))
		copy(result, typed)
		return result
	}
	items := sliceValue(value)
	result := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if image := mapValue(raw); image != nil && stringScalar(image["url"]) != "" {
			result = append(result, image)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func limitBilibiliHTML(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return ""
	}
	if bilibiliHTMLVisibleLength(value) <= limit {
		return value
	}
	var output strings.Builder
	openTags := make([]string, 0, 4)
	visible := 0
	truncated := false
	for offset := 0; offset < len(value); {
		if value[offset] == '<' {
			end := strings.IndexByte(value[offset:], '>')
			if end >= 0 {
				end += offset
				rawTag := value[offset : end+1]
				output.WriteString(rawTag)
				name, closing, selfClosing := bilibiliHTMLTag(rawTag)
				if name != "" && !bilibiliHTMLVoidTags[name] {
					if closing {
						for index := len(openTags) - 1; index >= 0; index-- {
							if openTags[index] == name {
								openTags = openTags[:index]
								break
							}
						}
					} else if !selfClosing {
						openTags = append(openTags, name)
					}
				}
				offset = end + 1
				continue
			}
		}
		if value[offset] == '&' {
			if end := strings.IndexByte(value[offset:], ';'); end > 0 && end <= 32 {
				end += offset
				entity := value[offset : end+1]
				decoded := html.UnescapeString(entity)
				length := utf8.RuneCountInString(decoded)
				if decoded != entity && visible+length <= limit {
					output.WriteString(entity)
					visible += length
					offset = end + 1
					continue
				}
			}
		}
		runeValue, size := utf8.DecodeRuneInString(value[offset:])
		if visible >= limit {
			truncated = true
			break
		}
		output.WriteRune(runeValue)
		visible++
		offset += size
	}
	if visible >= limit {
		truncated = true
	}
	if truncated {
		output.WriteString("...")
	}
	for index := len(openTags) - 1; index >= 0; index-- {
		output.WriteString("</" + openTags[index] + ">")
	}
	return output.String()
}

var bilibiliHTMLVoidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

func bilibiliHTMLTag(raw string) (name string, closing, selfClosing bool) {
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">"))
	if content == "" || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "?") {
		return "", false, true
	}
	closing = strings.HasPrefix(content, "/")
	content = strings.TrimSpace(strings.TrimPrefix(content, "/"))
	selfClosing = strings.HasSuffix(content, "/")
	content = strings.TrimSpace(strings.TrimSuffix(content, "/"))
	if index := strings.IndexAny(content, " \t\r\n"); index >= 0 {
		content = content[:index]
	}
	return strings.ToLower(content), closing, selfClosing
}

func bilibiliHTMLVisibleLength(value string) int {
	visible := 0
	for offset := 0; offset < len(value); {
		if value[offset] == '<' {
			if end := strings.IndexByte(value[offset:], '>'); end >= 0 {
				offset += end + 1
				continue
			}
		}
		if value[offset] == '&' {
			if end := strings.IndexByte(value[offset:], ';'); end > 0 && end <= 32 {
				entity := value[offset : offset+end+1]
				decoded := html.UnescapeString(entity)
				if decoded != entity {
					visible += utf8.RuneCountInString(decoded)
					offset += end + 1
					continue
				}
			}
		}
		_, size := utf8.DecodeRuneInString(value[offset:])
		visible++
		offset += size
	}
	return visible
}

func buildMediaItems(images []map[string]any, duration, service string) []map[string]any {
	result := make([]map[string]any, 0, len(images))
	for index, image := range images {
		imageURL := stringScalar(image["url"])
		if imageURL == "" {
			continue
		}
		width := intScalar(image["width"])
		height := intScalar(image["height"])
		isGIF := strings.HasSuffix(strings.ToLower(strings.Split(imageURL, "?")[0]), ".gif")
		isLong := width > 0 && height > width*2
		labels := make([]string, 0, 2)
		classes := []string{"media-item"}
		if service == "视频" || service == "直播" || service == "文章" {
			classes = append(classes, "media-item--wide")
		}
		if isGIF {
			labels = append(labels, "动图")
			classes = append(classes, "media-item--gif")
		}
		if isLong {
			labels = append(labels, "长图")
			classes = append(classes, "media-item--long")
		}
		itemDuration := stringScalar(image["duration_text"])
		if index == 0 && itemDuration == "" && service == "视频" {
			itemDuration = strings.TrimSpace(duration)
		}
		if itemDuration != "" {
			classes = append(classes, "media-item--video")
		}
		fallback := "assets/grid.svg"
		if service == "视频" || service == "直播" || service == "文章" {
			fallback = "assets/cover.svg"
		}
		result = append(result, map[string]any{
			"url": imageURL, "class": strings.Join(classes, " "), "label": strings.Join(labels, " · "),
			"duration_text": itemDuration, "width": width, "height": height,
			"fallback": fallback,
		})
	}
	return result
}

func mediaGridClass(count int) string {
	switch count {
	case 0:
		return ""
	case 1:
		return "media-grid--single"
	case 2, 4:
		return "media-grid--double"
	default:
		return "media-grid--triple"
	}
}

func bilibiliServiceLabel(service string) string {
	if label := bilibiliServiceLabels[service]; label != "" {
		return label
	}
	if service != "" {
		return service
	}
	return "内容"
}

func subscriptionUpdateKey(item subscription, update map[string]any) string {
	return fmt.Sprintf("seen:%s:%s:%s", item.ID, stringScalar(update["service"]), stringScalar(update["id"]))
}
