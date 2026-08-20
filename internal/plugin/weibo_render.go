package plugin

import "strings"

func buildWeiboRenderData(item subscription, update map[string]any) map[string]any {
	subscriberCards := buildSubscriberCards(item.Subscribers)
	author := renderSubscriptionAuthor(item, update["author"])
	rawSummary := stringScalar(update["summary"])
	title := weiboDistinctTitle(update["title"], rawSummary)
	summary := cleanText(rawSummary)
	service := weiboServiceLabel(stringScalar(update["service"]))
	category := firstText(update["category"], weiboServiceCategory(stringScalar(update["service"])))
	images := imageMaps(update["images"], 9)
	mediaItems := buildMediaItems(images, stringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": "",
		"subtitle": "微博 · " + service, "source_label": category,
		"platform": "微博", "service": service, "category": category,
		"author": author, "author_uid_text": uidText(firstText(author["uid"], item.UID)),
		"summary": summary, "images": images, "image_count": len(mediaItems),
		"media_grid_class": mediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": stringScalar(update["duration_text"]), "url": stringScalar(update["url"]),
		"pub_ts": intScalar(update["pub_ts"]), "created_at": stringScalar(update["created_at"]),
		"original":     renderWeiboOriginal(update["original"]),
		"subscription": map[string]any{"uid": item.UID, "name": firstText(item.Name, item.UID)},
		"subscribers":  item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": subscriberNames(subscriberCards),
	}
}

func buildWeiboFallback(data map[string]any) string {
	lines := []string{
		firstText(data["source_label"], data["subtitle"], "微博订阅更新"),
		firstText(data["headline"], data["title"]),
		firstText(data["content_text"], data["summary"]),
		weiboOriginalFallback(data["original"]),
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

func renderWeiboOriginal(value any) map[string]any {
	original := mapValue(value)
	if original == nil {
		return nil
	}
	service := weiboServiceLabel(stringScalar(original["service"]))
	rawSummary := stringScalar(original["summary"])
	images := imageMaps(original["images"], 6)
	mediaItems := buildMediaItems(images, stringScalar(original["duration_text"]), service)
	author := mapValue(original["author"])
	return map[string]any{
		"title":   weiboDistinctTitle(original["title"], rawSummary),
		"service": service, "category": firstText(original["category"], service), "author": author,
		"author_uid_text": uidText(stringScalar(author["uid"])),
		"summary":         cleanText(rawSummary),
		"images":          imageMaps(images, 3),
		"image_count":     len(mediaItems), "media_grid_class": mediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": stringScalar(original["duration_text"]), "url": stringScalar(original["url"]), "created_at": stringScalar(original["created_at"]),
	}
}

func weiboOriginalFallback(value any) string {
	original := mapValue(value)
	if original == nil {
		return ""
	}
	parts := []string{"原微博", stringScalar(original["title"]), stringScalar(original["summary"])}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return strings.Join(result, "\n")
}

func weiboServiceLabel(service string) string {
	switch service {
	case "post":
		return "文字"
	case "image":
		return "图片"
	case "video":
		return "视频"
	case "repost":
		return "转发"
	}
	if label := services["weibo"].names[service]; label != "" && service != "all" {
		return label
	}
	if service != "" {
		return service
	}
	return "内容"
}
