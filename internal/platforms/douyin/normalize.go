package douyin

import (
	"net/url"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func normalizeDouyinAweme(aweme map[string]any) map[string]any {
	if aweme == nil {
		return nil
	}
	id := plugin.FirstText(aweme["aweme_id"], aweme["awemeId"], aweme["group_id"])
	if id == "" {
		return nil
	}
	author := plugin.MapValue(aweme["author"])
	uid := plugin.FirstText(author["sec_uid"], author["sec_user_id"], aweme["sec_uid"])
	name := plugin.FirstText(author["nickname"], author["nick_name"], author["name"])
	service := douyinAwemeService(aweme)
	summary := strings.TrimSpace(plugin.FirstText(aweme["desc"], aweme["description"], aweme["caption"]))
	title := douyinDistinctTitle(plugin.NestedValue(aweme, "preview_title"), plugin.NestedValue(aweme, "share_info", "share_title"), summary)
	pubTS := douyinPubTS(aweme)
	images := douyinAwemeImages(aweme, service)
	duration := douyinAwemeDuration(aweme)
	return map[string]any{
		"id": id, "platform": "douyin", "uid": uid, "service": service,
		"category": douyinServiceCategory(service), "title": title,
		"summary": summary, "url": douyinAwemeURL(aweme, id, service),
		"pub_ts": pubTS, "created_at": plugin.FormatTime(pubTS, ""),
		"duration_text": duration,
		"author": map[string]any{
			"name": plugin.FirstText(name, uid), "uid": uid,
			"unique_id": plugin.FirstText(author["unique_id"], author["short_id"]),
			"avatar":    douyinURLFromImage(plugin.FirstNonNil(author["avatar_larger"], author["avatar_medium"], author["avatar_thumb"], author["avatar"])),
		},
		"images": images,
	}
}

func normalizeDouyinLive(live map[string]any, secUID string) map[string]any {
	if live == nil {
		return nil
	}
	id := plugin.FirstText(live["id"], live["web_rid"], live["room_id"])
	if id == "" {
		return nil
	}
	user := plugin.MapValue(live["user"])
	uid := plugin.FirstText(user["sec_uid"], user["sec_user_id"], secUID)
	name := plugin.FirstText(user["nickname"], user["name"])
	title := plugin.FirstText(live["title"], "直播中")
	cover := plugin.FirstText(live["cover"])
	images := []map[string]any{}
	if cover != "" {
		images = append(images, map[string]any{"url": cover})
	}
	pubTS := time.Now().Unix()
	return map[string]any{
		"id": "live:" + id, "platform": "douyin", "uid": uid, "service": "live",
		"category": douyinServiceCategory("live"), "title": title,
		"summary": title, "url": plugin.FirstText(live["url"], "https://live.douyin.com/"+url.PathEscape(id)),
		"pub_ts": pubTS, "created_at": plugin.FormatTime(pubTS, ""),
		"author": map[string]any{
			"name": plugin.FirstText(name, uid), "uid": uid,
			"unique_id": plugin.FirstText(user["unique_id"], user["short_id"]),
			"avatar":    douyinURLFromImage(plugin.FirstNonNil(user["avatar_larger"], user["avatar_thumb"], user["avatar"])),
		},
		"images": images,
	}
}

func douyinAwemeService(aweme map[string]any) string {
	awemeType := int(plugin.IntScalar(aweme["aweme_type"]))
	switch awemeType {
	case 2, 68, 101, 107, 151:
		return "image_text"
	}
	if len(plugin.SliceValue(aweme["images"])) > 0 && plugin.MapValue(aweme["video"]) == nil {
		return "image_text"
	}
	if plugin.MapValue(aweme["image_post_info"]) != nil && plugin.MapValue(aweme["video"]) == nil {
		return "image_text"
	}
	return "video"
}

func douyinServiceCategory(service string) string {
	switch service {
	case "image_text":
		return "图文"
	case "live":
		return "直播"
	case "video":
		return "视频"
	default:
		return "作品"
	}
}

func douyinServiceLabel(service string) string {
	return douyinServiceCategory(service)
}

func douyinDistinctTitle(values ...any) string {
	summary := ""
	if len(values) > 0 {
		summary = strings.TrimSpace(plugin.StringScalar(values[len(values)-1]))
	}
	for _, value := range values[:len(values)-1] {
		title := strings.TrimSpace(plugin.StringScalar(value))
		if title == "" || title == summary {
			continue
		}
		return plugin.TruncateRunes(plugin.CleanText(title), 72)
	}
	return ""
}

func douyinPubTS(aweme map[string]any) int64 {
	timestamp := plugin.IntScalar(plugin.FirstNonNil(aweme["create_time"], aweme["createTime"], aweme["create_time_sec"]))
	if timestamp <= 0 {
		return 0
	}
	if timestamp > 1e12 {
		timestamp = timestamp / 1000
	}
	return timestamp
}

func douyinAwemeURL(aweme map[string]any, id, service string) string {
	if share := plugin.FirstText(aweme["share_url"], plugin.NestedValue(aweme, "share_info", "share_url")); share != "" {
		return share
	}
	if service == "image_text" {
		return "https://www.douyin.com/note/" + url.PathEscape(id)
	}
	return "https://www.douyin.com/video/" + url.PathEscape(id)
}

func douyinAwemeImages(aweme map[string]any, service string) []map[string]any {
	images := make([]map[string]any, 0)
	appendURL := func(raw any) {
		text := douyinURLFromImage(raw)
		if text == "" {
			return
		}
		images = append(images, map[string]any{"url": text})
	}
	if service == "image_text" {
		for _, item := range plugin.SliceValue(aweme["images"]) {
			appendURL(item)
		}
		for _, item := range plugin.SliceValue(plugin.NestedValue(aweme, "image_post_info", "images")) {
			object := plugin.MapValue(item)
			appendURL(plugin.FirstNonNil(object["display_image"], object["origin_image"], item))
		}
	}
	if len(images) == 0 {
		video := plugin.MapValue(aweme["video"])
		appendURL(plugin.FirstNonNil(video["origin_cover"], video["cover"], video["dynamic_cover"], aweme["cover"], aweme["video_cover"]))
	}
	if len(images) > 9 {
		images = images[:9]
	}
	return images
}

func douyinAwemeDuration(aweme map[string]any) string {
	duration := plugin.IntScalar(plugin.NestedValue(aweme, "video", "duration"))
	if duration <= 0 {
		duration = plugin.IntScalar(aweme["duration"])
	}
	if duration <= 0 {
		return ""
	}
	if duration > 1000 {
		duration = duration / 1000
	}
	minutes := duration / 60
	seconds := duration % 60
	return strings.TrimSpace(plugin.StringScalar(minutes) + ":" + padTwoDigits(int(seconds)))
}

func padTwoDigits(value int) string {
	if value < 10 {
		return "0" + plugin.StringScalar(value)
	}
	return plugin.StringScalar(value)
}
