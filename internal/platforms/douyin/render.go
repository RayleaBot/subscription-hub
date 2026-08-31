package douyin

import (
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func buildDouyinRenderData(item plugin.Subscription, update map[string]any) map[string]any {
	subscriberCards := plugin.BuildSubscriberCards(item.Subscribers)
	author := plugin.RenderSubscriptionAuthor(item, update["author"])
	rawSummary := plugin.StringScalar(update["summary"])
	title := douyinDistinctTitle(update["title"], rawSummary)
	summary := plugin.CleanText(rawSummary)
	service := douyinServiceLabel(plugin.StringScalar(update["service"]))
	category := plugin.FirstText(update["category"], douyinServiceCategory(plugin.StringScalar(update["service"])))
	images := plugin.ImageMaps(update["images"], 9)
	mediaItems := plugin.BuildMediaItems(images, plugin.StringScalar(update["duration_text"]), service)
	return map[string]any{
		"title": title, "headline": title,
		"content_text": summary, "content_html": "",
		"subtitle": "抖音 · " + service, "source_label": category,
		"platform": "抖音", "service": service, "category": category,
		// 作者标识优先展示抖音号（可被用户修改）原值；缺失时留空，模板回退
		// sec_uid 并加 "UID " 前缀。
		"author": author, "author_uid_text": plugin.FirstText(author["unique_id"], item.UniqueID),
		"summary": summary, "images": images, "image_count": len(mediaItems),
		"metrics":          buildDouyinMetrics(update),
		"media_grid_class": plugin.MediaGridClass(len(mediaItems)), "media_items": mediaItems,
		"duration_text": plugin.StringScalar(update["duration_text"]), "url": plugin.StringScalar(update["url"]),
		"pub_ts": plugin.IntScalar(update["pub_ts"]), "created_at": plugin.StringScalar(update["created_at"]),
		"subscription": map[string]any{"uid": item.UID, "name": plugin.FirstText(item.Name, item.UID)},
		"subscribers":  item.Subscribers, "subscriber_cards": subscriberCards, "subscriber_text": plugin.SubscriberNames(subscriberCards),
	}
}

func buildDouyinMetrics(update map[string]any) []map[string]any {
	stats := plugin.MapValue(update["stats"])
	if stats == nil {
		return nil
	}
	metrics := make([]map[string]any, 0, 5)
	specs := []struct {
		key, label string
		value      any
	}{
		{key: "like", label: "点赞", value: stats["like"]},
		{key: "favorite", label: "收藏", value: stats["favorite"]},
		{key: "comment", label: "评论", value: stats["comment"]},
		{key: "share", label: "分享", value: stats["share"]},
		{key: "viewers", label: "观看", value: stats["viewers"]},
	}
	for _, spec := range specs {
		if metric := plugin.BuildContentMetric(spec.key, spec.label, spec.value); metric != nil {
			metrics = append(metrics, metric)
		}
	}
	return metrics
}

func buildDouyinFallback(data map[string]any) string {
	lines := []string{
		plugin.FirstText(data["source_label"], data["subtitle"], "抖音订阅更新"),
		plugin.FirstText(data["headline"], data["title"]),
		plugin.FirstText(data["content_text"], data["summary"]),
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
