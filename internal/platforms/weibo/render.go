package weibo

import (
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func buildWeiboRenderData(item plugin.Subscription, update map[string]any) map[string]any {
	subscriberCards := plugin.BuildSubscriberCards(item.Subscribers)
	author := plugin.RenderSubscriptionAuthor(item, update["author"])
	rawSummary := plugin.StringScalar(update["summary"])
	title := weiboDistinctTitle(update["title"], rawSummary)
	summary := plugin.CleanText(rawSummary)
	service := weiboServiceLabel(plugin.StringScalar(update["service"]))
	category := plugin.FirstText(update["category"], weiboServiceCategory(plugin.StringScalar(update["service"])))
	images := plugin.ImageMaps(update["images"], 9)
	mediaItems := plugin.BuildMediaItems(images, plugin.StringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": "",
		"subtitle": "微博 · " + service, "source_label": category,
		"platform": "微博", "service": service, "category": category,
		"author": author, "author_uid_text": plugin.UIDText(plugin.FirstText(author["uid"], item.UID)),
		"summary": summary, "images": images, "image_count": len(mediaItems),
		"metrics":          buildWeiboMetrics(update),
		"media_grid_class": plugin.MediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": plugin.StringScalar(update["duration_text"]), "url": plugin.StringScalar(update["url"]),
		"pub_ts": plugin.IntScalar(update["pub_ts"]), "created_at": plugin.StringScalar(update["created_at"]),
		"original":     renderWeiboOriginal(update["original"]),
		"subscription": map[string]any{"uid": item.UID, "name": plugin.FirstText(item.Name, item.UID)},
		"subscribers":  item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": plugin.SubscriberNames(subscriberCards),
	}
}

func buildWeiboMetrics(update map[string]any) []map[string]any {
	stats := plugin.MapValue(update["stats"])
	if stats == nil {
		return nil
	}
	metrics := make([]map[string]any, 0, 3)
	for _, spec := range []struct {
		key, label string
		value      any
	}{
		{key: "repost", label: "转发", value: stats["repost"]},
		{key: "comment", label: "评论", value: stats["comment"]},
		{key: "like", label: "点赞", value: stats["like"]},
	} {
		if metric := plugin.BuildContentMetric(spec.key, spec.label, spec.value); metric != nil {
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

func buildWeiboFallback(data map[string]any) string {
	lines := []string{
		plugin.FirstText(data["source_label"], data["subtitle"], "微博订阅更新"),
		plugin.FirstText(data["headline"], data["title"]),
		plugin.FirstText(data["content_text"], data["summary"]),
		weiboOriginalFallback(data["original"]),
	}
	if subscribers := plugin.StringScalar(data["subscriber_text"]); subscribers != "" {
		lines = append(lines, "订阅人："+subscribers)
	}
	lines = append(lines, plugin.StringScalar(data["url"]))
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return strings.Join(result, "\n")
}

func renderWeiboOriginal(value any) map[string]any {
	original := plugin.MapValue(value)
	if original == nil {
		return nil
	}
	service := weiboServiceLabel(plugin.StringScalar(original["service"]))
	rawSummary := plugin.StringScalar(original["summary"])
	images := plugin.ImageMaps(original["images"], 6)
	mediaItems := plugin.BuildMediaItems(images, plugin.StringScalar(original["duration_text"]), service)
	author := plugin.MapValue(original["author"])
	return map[string]any{
		"title":   weiboDistinctTitle(original["title"], rawSummary),
		"service": service, "category": plugin.FirstText(original["category"], service), "author": author,
		"author_uid_text": plugin.UIDText(plugin.StringScalar(author["uid"])),
		"summary":         plugin.CleanText(rawSummary),
		"images":          plugin.ImageMaps(images, 3),
		"image_count":     len(mediaItems), "media_grid_class": plugin.MediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": plugin.StringScalar(original["duration_text"]), "url": plugin.StringScalar(original["url"]), "created_at": plugin.StringScalar(original["created_at"]),
	}
}

func weiboOriginalFallback(value any) string {
	original := plugin.MapValue(value)
	if original == nil {
		return ""
	}
	parts := []string{"原微博", plugin.StringScalar(original["title"]), plugin.StringScalar(original["summary"])}
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
	if label := catalog.Label(service); label != "" && service != "all" {
		return label
	}
	if service != "" {
		return service
	}
	return "内容"
}
