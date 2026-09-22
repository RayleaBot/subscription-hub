package douyin

import (
	"net/url"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func normalizeDouyinAweme(location *time.Location, aweme map[string]any) map[string]any {
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
		"pub_ts": pubTS, "created_at": plugin.FormatTime(location, pubTS, ""),
		"duration_text": duration,
		"author": map[string]any{
			"name": plugin.FirstText(name, uid), "uid": uid,
			"unique_id": plugin.FirstText(author["unique_id"], author["short_id"]),
			"avatar":    douyinURLFromImage(plugin.FirstNonNil(author["avatar_larger"], author["avatar_medium"], author["avatar_thumb"], author["avatar"])),
		},
		"images":          images,
		"stats":           mergeDouyinStats(plugin.MapValue(plugin.FirstNonNil(aweme["statistics"], aweme["statistics_info"]))),
		"_resolver_aweme": aweme,
	}
}

func normalizeDouyinLive(location *time.Location, live map[string]any, secUID string) map[string]any {
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
	pubTS := plugin.IntScalar(live["start_time"])
	if pubTS <= 0 {
		pubTS = time.Now().Unix()
	}
	liveStatus := plugin.IntScalar(plugin.FirstNonNil(live["live_status"], 1))
	summary := title
	if liveStatus != 1 {
		summary = "直播已结束"
	}
	liveURL := plugin.FirstText(live["url"], "https://live.douyin.com/"+url.PathEscape(id))
	if cleaned, ok := cleanDouyinShareURL(liveURL); ok {
		liveURL = cleaned
	}
	return map[string]any{
		"id": "live:" + id, "platform": "douyin", "uid": uid, "service": "live",
		"category": douyinServiceCategory("live"), "title": title,
		"summary": summary, "url": liveURL, "live_status": liveStatus,
		"pub_ts": pubTS, "created_at": plugin.FormatTime(location, pubTS, ""),
		"author": map[string]any{
			"name": plugin.FirstText(name, uid), "uid": uid,
			"unique_id": plugin.FirstText(user["unique_id"], user["short_id"]),
			"avatar":    douyinURLFromImage(plugin.FirstNonNil(user["avatar_larger"], user["avatar_thumb"], user["avatar"])),
		},
		"images": images,
		"stats": map[string]any{
			"viewers": plugin.FirstNonNil(live["user_count"], plugin.NestedValue(live, "room_view_stats", "total_user"), plugin.NestedValue(live, "stats", "user_count")),
		},
		"_resolver_live": live,
	}
}

func mergeDouyinStats(stats map[string]any) map[string]any {
	if stats == nil {
		return nil
	}
	return map[string]any{
		"like":     plugin.FirstNonNil(stats["digg_count"], stats["like_count"], stats["diggCount"]),
		"favorite": plugin.FirstNonNil(stats["collect_count"], stats["collectCount"]),
		"comment":  plugin.FirstNonNil(stats["comment_count"], stats["commentCount"]),
		"share":    plugin.FirstNonNil(stats["share_count"], stats["shareCount"]),
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
		if cleaned, ok := cleanDouyinShareURL(share); ok {
			return cleaned
		}
	}
	if service == "image_text" {
		return "https://www.douyin.com/note/" + url.PathEscape(id)
	}
	return "https://www.douyin.com/video/" + url.PathEscape(id)
}

// cleanDouyinShareURL 去掉抖音分享链接的站内追踪参数（share_uid、tt_from、
// utm_source 等）：作品/直播标识都在路径里，查询串只是渠道标记，直接丢弃。
// 非抖音主机返回 ok=false，交由调用方回退到规范链接。
func cleanDouyinShareURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	isDouyinHost := host == "douyin.com" || host == "v.douyin.com" || host == "live.douyin.com" ||
		host == "iesdouyin.com" || strings.HasSuffix(host, ".douyin.com") || strings.HasSuffix(host, ".iesdouyin.com")
	if !isDouyinHost {
		return "", false
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func douyinAwemeImages(aweme map[string]any, service string) []map[string]any {
	images := make([]map[string]any, 0)
	appendMirrors := func(raw any) {
		mirrors := douyinImageMirrors(raw)
		if len(mirrors) == 0 {
			return
		}
		images = append(images, map[string]any{"url": mirrors[0], "candidates": mirrors})
	}
	if service == "image_text" {
		for _, item := range plugin.SliceValue(aweme["images"]) {
			appendMirrors(item)
		}
		for _, item := range plugin.SliceValue(plugin.NestedValue(aweme, "image_post_info", "images")) {
			object := plugin.MapValue(item)
			appendMirrors(plugin.FirstNonNil(object["display_image"], object["origin_image"], item))
		}
	}
	if len(images) == 0 {
		// cover_original_scale / cover 是作品发布封面；origin_cover 可能只是视频帧。
		// 按可用地址依次回退，空封面对象不能阻断后续候选；动态封面放最后。
		video := plugin.MapValue(aweme["video"])
		for _, cover := range []any{video["cover_original_scale"], video["cover"], aweme["cover"], aweme["video_cover"], video["origin_cover"], video["dynamic_cover"]} {
			appendMirrors(cover)
			if len(images) > 0 {
				break
			}
		}
	}
	if len(images) > 9 {
		images = images[:9]
	}
	return images
}

// douyinImageMirrors 收集图片/封面对象里的镜像地址（url_list），最多 4 个。
// 抖音 CDN 按区域主机（p3/p6/p9/p26…）存放同一张图，url_list 即各区域镜像，
// 签名与主机无关，任意一个成功即可，用列表整体作为取图重试候选。
func douyinImageMirrors(raw any) []string {
	mirrors := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	add := func(value any) {
		text := douyinURLFromImage(value)
		if text == "" || !strings.HasPrefix(text, "https://") || seen[text] {
			return
		}
		seen[text] = true
		mirrors = append(mirrors, text)
	}
	if object := plugin.MapValue(raw); object != nil {
		for _, item := range plugin.SliceValue(object["url_list"]) {
			add(item)
			if len(mirrors) == 4 {
				return mirrors
			}
		}
	}
	if len(mirrors) == 0 {
		add(raw)
	}
	return mirrors
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
