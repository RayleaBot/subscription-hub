package plugin

import "strings"

func buildDouyinRenderData(item subscription, update map[string]any) map[string]any {
	subscriberCards := buildSubscriberCards(item.Subscribers)
	author := renderSubscriptionAuthor(item, update["author"])
	rawSummary := stringScalar(update["summary"])
	title := douyinDistinctTitle(update["title"], rawSummary)
	summary := cleanText(rawSummary)
	service := douyinServiceLabel(stringScalar(update["service"]))
	category := firstText(update["category"], douyinServiceCategory(stringScalar(update["service"])))
	images := imageMaps(update["images"], 9)
	mediaItems := buildMediaItems(images, stringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": "",
		"subtitle": "抖音 · " + service, "source_label": category,
		"platform": "抖音", "service": service, "category": category,
		// 作者标识优先展示抖音号（可被用户修改）原值；缺失时留空，模板回退
		// sec_uid 并加 "UID " 前缀。
		"author": author, "author_uid_text": firstText(author["unique_id"], item.UniqueID),
		"summary": summary, "images": images, "image_count": len(mediaItems),
		"media_grid_class": mediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": stringScalar(update["duration_text"]), "url": stringScalar(update["url"]),
		"pub_ts": intScalar(update["pub_ts"]), "created_at": stringScalar(update["created_at"]),
		"subscription": map[string]any{"uid": item.UID, "name": firstText(item.Name, item.UID)},
		"subscribers":  item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": subscriberNames(subscriberCards),
	}
}

func buildDouyinFallback(data map[string]any) string {
	lines := []string{
		firstText(data["source_label"], data["subtitle"], "抖音订阅更新"),
		firstText(data["headline"], data["title"]),
		firstText(data["content_text"], data["summary"]),
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
