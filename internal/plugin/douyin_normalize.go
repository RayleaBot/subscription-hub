package plugin

import (
	"net/url"
	"strings"
	"time"
)

func normalizeDouyinAweme(aweme map[string]any) map[string]any {
	if aweme == nil {
		return nil
	}
	id := firstText(aweme["aweme_id"], aweme["awemeId"], aweme["group_id"])
	if id == "" {
		return nil
	}
	author := mapValue(aweme["author"])
	uid := firstText(author["sec_uid"], author["sec_user_id"], aweme["sec_uid"])
	name := firstText(author["nickname"], author["nick_name"], author["name"])
	service := douyinAwemeService(aweme)
	summary := strings.TrimSpace(firstText(aweme["desc"], aweme["description"], aweme["caption"]))
	title := douyinDistinctTitle(nestedValue(aweme, "preview_title"), nestedValue(aweme, "share_info", "share_title"), summary)
	pubTS := douyinPubTS(aweme)
	images := douyinAwemeImages(aweme, service)
	duration := douyinAwemeDuration(aweme)
	return map[string]any{
		"id": id, "platform": "douyin", "uid": uid, "service": service,
		"category": douyinServiceCategory(service), "title": title,
		"summary": summary, "url": douyinAwemeURL(aweme, id, service),
		"pub_ts": pubTS, "created_at": formatBilibiliTime(pubTS, ""),
		"duration_text": duration,
		"author": map[string]any{
			"name": firstText(name, uid), "uid": uid,
			"unique_id": firstText(author["unique_id"], author["short_id"]),
			"avatar":    douyinURLFromImage(firstNonNil(author["avatar_larger"], author["avatar_medium"], author["avatar_thumb"], author["avatar"])),
		},
		"images": images,
	}
}

func normalizeDouyinLive(live map[string]any, secUID string) map[string]any {
	if live == nil {
		return nil
	}
	id := firstText(live["id"], live["web_rid"], live["room_id"])
	if id == "" {
		return nil
	}
	user := mapValue(live["user"])
	uid := firstText(user["sec_uid"], user["sec_user_id"], secUID)
	name := firstText(user["nickname"], user["name"])
	title := firstText(live["title"], "直播中")
	cover := firstText(live["cover"])
	images := []map[string]any{}
	if cover != "" {
		images = append(images, map[string]any{"url": cover})
	}
	pubTS := time.Now().Unix()
	return map[string]any{
		"id": "live:" + id, "platform": "douyin", "uid": uid, "service": "live",
		"category": douyinServiceCategory("live"), "title": title,
		"summary": title, "url": firstText(live["url"], "https://live.douyin.com/"+url.PathEscape(id)),
		"pub_ts": pubTS, "created_at": formatBilibiliTime(pubTS, ""),
		"author": map[string]any{
			"name": firstText(name, uid), "uid": uid,
			"unique_id": firstText(user["unique_id"], user["short_id"]),
			"avatar":    douyinURLFromImage(firstNonNil(user["avatar_larger"], user["avatar_thumb"], user["avatar"])),
		},
		"images": images,
	}
}

func douyinAwemeService(aweme map[string]any) string {
	awemeType := int(intScalar(aweme["aweme_type"]))
	switch awemeType {
	case 2, 68, 101, 107, 151:
		return "image_text"
	}
	if len(sliceValue(aweme["images"])) > 0 && mapValue(aweme["video"]) == nil {
		return "image_text"
	}
	if mapValue(aweme["image_post_info"]) != nil && mapValue(aweme["video"]) == nil {
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
		summary = strings.TrimSpace(stringScalar(values[len(values)-1]))
	}
	for _, value := range values[:len(values)-1] {
		title := strings.TrimSpace(stringScalar(value))
		if title == "" || title == summary {
			continue
		}
		return truncateRunes(cleanText(title), 72)
	}
	return ""
}

func douyinPubTS(aweme map[string]any) int64 {
	timestamp := intScalar(firstNonNil(aweme["create_time"], aweme["createTime"], aweme["create_time_sec"]))
	if timestamp <= 0 {
		return 0
	}
	if timestamp > 1e12 {
		timestamp = timestamp / 1000
	}
	return timestamp
}

func douyinAwemeURL(aweme map[string]any, id, service string) string {
	if share := firstText(aweme["share_url"], nestedValue(aweme, "share_info", "share_url")); share != "" {
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
		for _, item := range sliceValue(aweme["images"]) {
			appendURL(item)
		}
		for _, item := range sliceValue(nestedValue(aweme, "image_post_info", "images")) {
			object := mapValue(item)
			appendURL(firstNonNil(object["display_image"], object["origin_image"], item))
		}
	}
	if len(images) == 0 {
		video := mapValue(aweme["video"])
		appendURL(firstNonNil(video["origin_cover"], video["cover"], video["dynamic_cover"], aweme["cover"], aweme["video_cover"]))
	}
	if len(images) > 9 {
		images = images[:9]
	}
	return images
}

func douyinAwemeDuration(aweme map[string]any) string {
	duration := intScalar(nestedValue(aweme, "video", "duration"))
	if duration <= 0 {
		duration = intScalar(aweme["duration"])
	}
	if duration <= 0 {
		return ""
	}
	if duration > 1000 {
		duration = duration / 1000
	}
	minutes := duration / 60
	seconds := duration % 60
	return strings.TrimSpace(stringScalar(minutes) + ":" + padTwoDigits(int(seconds)))
}

func padTwoDigits(value int) string {
	if value < 10 {
		return "0" + stringScalar(value)
	}
	return stringScalar(value)
}
