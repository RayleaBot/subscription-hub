package bilibili

import (
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

var bilibiliServiceLabels = map[string]string{
	"live": "直播", "video": "视频", "image_text": "图文", "article": "文章", "repost": "转发", "all": "全部",
}

func buildBilibiliRenderData(item plugin.Subscription, update map[string]any) map[string]any {
	subscriberCards := plugin.BuildSubscriberCards(item.Subscribers)
	author := plugin.RenderSubscriptionAuthor(item, update["author"])
	title := plugin.TruncateRunes(plugin.FirstText(update["title"], "订阅更新"), 72)
	summary := plugin.TruncateRunes(plugin.StringScalar(update["summary"]), 420)
	summaryHTML := plugin.LimitHTML(plugin.StringScalar(update["summary_html"]), 420)
	service := bilibiliServiceLabel(plugin.StringScalar(update["service"]))
	category := plugin.FirstText(update["category"], service)
	images := plugin.ImageMaps(update["images"], 9)
	mediaItems := plugin.BuildMediaItems(images, plugin.StringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": summaryHTML,
		"subtitle": "Bilibili · " + service, "source_label": "Bilibili · " + category,
		"platform": "Bilibili", "service": service, "category": category,
		"author": author, "author_uid_text": plugin.UIDText(plugin.FirstText(author["uid"], item.UID)),
		"summary": summary, "summary_html": summaryHTML, "topic": renderTopic(update["topic"]),
		"images": images, "image_count": len(mediaItems), "media_grid_class": plugin.MediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": plugin.StringScalar(update["duration_text"]), "url": plugin.StringScalar(update["url"]),
		"pub_ts": plugin.IntScalar(update["pub_ts"]), "created_at": plugin.StringScalar(update["created_at"]),
		"live_event": plugin.StringScalar(update["live_event"]), "live_status": update["live_status"],
		"status_label": plugin.StringScalar(update["status_label"]), "live_started_at": plugin.StringScalar(update["live_started_at"]),
		"live_detected_at": plugin.StringScalar(update["live_detected_at"]),
		"original":         renderOriginal(update["original"]),
		"subscription":     map[string]any{"uid": item.UID, "name": plugin.FirstText(item.Name, item.UID)},
		"subscribers":      item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": plugin.SubscriberNames(subscriberCards),
	}
}

func buildBilibiliFallback(data map[string]any) string {
	lines := []string{
		plugin.FirstText(data["source_label"], data["subtitle"], "Bilibili 订阅更新"),
		plugin.FirstText(data["headline"], data["title"]),
		plugin.FirstText(data["content_text"], data["summary"]),
		originalFallback(data["original"]),
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

func renderOriginal(value any) map[string]any {
	original := plugin.MapValue(value)
	if original == nil {
		return nil
	}
	service := bilibiliServiceLabel(plugin.StringScalar(original["service"]))
	images := plugin.ImageMaps(original["images"], 6)
	mediaItems := plugin.BuildMediaItems(images, plugin.StringScalar(original["duration_text"]), service)
	author := plugin.MapValue(original["author"])
	return map[string]any{
		"title":   plugin.TruncateRunes(plugin.FirstText(original["title"], "原动态"), 72),
		"service": service, "category": plugin.FirstText(original["category"], service), "author": author,
		"author_uid_text": plugin.UIDText(plugin.StringScalar(author["uid"])),
		"summary":         plugin.TruncateRunes(plugin.StringScalar(original["summary"]), 260), "summary_html": plugin.LimitHTML(plugin.StringScalar(original["summary_html"]), 260),
		"topic": renderTopic(original["topic"]), "images": plugin.ImageMaps(images, 3),
		"image_count": len(mediaItems), "media_grid_class": plugin.MediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": plugin.StringScalar(original["duration_text"]), "url": plugin.StringScalar(original["url"]), "created_at": plugin.StringScalar(original["created_at"]),
	}
}

func originalFallback(value any) string {
	original := plugin.MapValue(value)
	if original == nil {
		return ""
	}
	parts := []string{"原动态", plugin.StringScalar(original["title"]), plugin.StringScalar(original["summary"])}
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
	name := plugin.StringScalar(topic["name"])
	return map[string]any{"name": name, "label": "# " + name, "url": plugin.StringScalar(topic["jump_url"])}
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
