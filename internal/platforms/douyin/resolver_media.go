package douyin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func (session *session) ResolverMedia(ctx context.Context, update plugin.Update, settings plugin.ResolverMediaSettings) (plugin.ResolverMediaPlan, error) {
	headers := map[string]string{"Referer": douyinWebReferer, "User-Agent": douyinUserAgent}
	if accounts, err := readDouyinAccounts(ctx, session.actions); err == nil && len(accounts) > 0 && accounts[0].Cookie != "" {
		headers["Cookie"] = accounts[0].Cookie
	}
	service := plugin.StringScalar(update["service"])
	if service == "live" {
		live := plugin.MapValue(update["_resolver_live"])
		streamURL := douyinResolverLiveURL(live)
		if streamURL == "" {
			return plugin.ResolverMediaPlan{}, errors.New("没有获取到抖音直播流")
		}
		return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{Kind: "live", URLs: []string{streamURL}, Headers: headers, FileName: "douyin-live.mp4"}}}, nil
	}
	aweme := plugin.MapValue(update["_resolver_aweme"])
	if aweme == nil {
		return plugin.ResolverMediaPlan{}, nil
	}
	duration := douyinResolverDurationSeconds(aweme)
	if service == "video" {
		if duration > settings.DouyinMaxDurationSeconds {
			return plugin.ResolverMediaPlan{}, &plugin.ResolverMediaSkippedError{Reason: fmt.Sprintf("视频时长 %d 秒，超过超级管理员设置的 %d 秒限制，不发送视频", duration, settings.DouyinMaxDurationSeconds)}
		}
		urls := douyinResolverVideoURLs(plugin.MapValue(aweme["video"]), settings.DouyinResolution)
		if len(urls) == 0 {
			return plugin.ResolverMediaPlan{}, errors.New("没有获取到抖音视频地址")
		}
		return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{Kind: "video", URLs: urls, Headers: headers, FileName: "douyin-" + plugin.StringScalar(update["id"]) + ".mp4", Duration: duration}}}, nil
	}

	musicURLs := douyinResolverPlayURLs(plugin.NestedValue(aweme, "music", "play_url"), settings.DouyinResolution)
	sources := make([]plugin.ResolverMediaSource, 0)
	for index, raw := range plugin.SliceValue(aweme["images"]) {
		image := plugin.MapValue(raw)
		video := plugin.FirstNonNil(plugin.NestedValue(image, "video", "play_addr_h264"), plugin.NestedValue(image, "video", "play_addr"))
		videoURLs := douyinResolverPlayURLs(video, settings.DouyinResolution)
		if len(videoURLs) > 0 {
			source := plugin.ResolverMediaSource{Kind: "video", URLs: videoURLs, Headers: headers, FileName: fmt.Sprintf("douyin-motion-%02d.mp4", index+1)}
			if settings.DouyinMergeBGM && len(musicURLs) > 0 {
				source.AudioURLs = musicURLs
				source.MergeAudio = true
			}
			sources = append(sources, source)
			continue
		}
		imageURLs := douyinImageMirrors(image)
		if len(imageURLs) > 0 {
			sources = append(sources, plugin.ResolverMediaSource{Kind: "image", URLs: imageURLs, Headers: headers, FileName: fmt.Sprintf("douyin-image-%02d.jpg", index+1)})
		}
	}
	return plugin.ResolverMediaPlan{Sources: sources}, nil
}

func douyinResolverDurationSeconds(aweme map[string]any) int {
	duration := int(plugin.IntScalar(plugin.FirstNonNil(plugin.NestedValue(aweme, "video", "duration"), aweme["duration"])))
	if duration > 1000 {
		duration /= 1000
	}
	return duration
}

func douyinResolverVideoURLs(video map[string]any, resolution int) []string {
	if video == nil {
		return nil
	}
	for _, raw := range plugin.SliceValue(video["bit_rate"]) {
		entry := plugin.MapValue(raw)
		gear := strings.ToLower(plugin.FirstText(entry["gear_name"], entry["quality_type"], entry["quality_type_name"]))
		if strings.Contains(gear, fmt.Sprint(resolution)) {
			if urls := douyinResolverPlayURLs(plugin.FirstNonNil(entry["play_addr"], entry["play_addr_265"]), resolution); len(urls) > 0 {
				return urls
			}
		}
	}
	return douyinResolverPlayURLs(plugin.FirstNonNil(video["play_addr"], video["play_addr_h264"], video["play_addr_265"]), resolution)
}

func douyinResolverPlayURLs(raw any, resolution int) []string {
	object := plugin.MapValue(raw)
	if object == nil {
		if value := plugin.StringScalar(raw); strings.HasPrefix(value, "http") {
			return []string{value}
		}
		return nil
	}
	result := make([]string, 0, 4)
	for _, item := range plugin.SliceValue(object["url_list"]) {
		if value := plugin.StringScalar(item); strings.HasPrefix(value, "http") {
			result = append(result, value)
		}
	}
	if uri := strings.TrimSpace(plugin.StringScalar(object["uri"])); uri != "" {
		result = append([]string{fmt.Sprintf("https://aweme.snssdk.com/aweme/v1/play/?video_id=%s&ratio=%dp&line=0", uri, resolution)}, result...)
	}
	return result
}

func douyinResolverLiveURL(live map[string]any) string {
	room := plugin.MapValue(live["room"])
	for _, candidate := range []any{
		plugin.NestedValue(room, "stream_url", "flv_pull_url", "FULL_HD1"),
		plugin.NestedValue(room, "stream_url", "flv_pull_url", "HD1"),
		plugin.NestedValue(room, "stream_url", "flv_pull_url", "SD1"),
		plugin.NestedValue(room, "stream_url", "hls_pull_url_map", "FULL_HD1"),
		plugin.NestedValue(room, "stream_url", "hls_pull_url_map", "HD1"),
	} {
		if value := plugin.StringScalar(candidate); strings.HasPrefix(value, "http") {
			return value
		}
	}
	pullData := plugin.StringScalar(plugin.NestedValue(room, "stream_url", "live_core_sdk_data", "pull_data", "stream_data"))
	if pullData != "" {
		var document any
		if json.Unmarshal([]byte(pullData), &document) == nil {
			if value := findDouyinStreamURL(document, 0); value != "" {
				return value
			}
		}
	}
	return ""
}

func findDouyinStreamURL(value any, depth int) string {
	if depth > 8 {
		return ""
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.Contains(strings.ToLower(key), "url") {
				if candidate := plugin.StringScalar(child); strings.HasPrefix(candidate, "http") {
					return candidate
				}
			}
			if candidate := findDouyinStreamURL(child, depth+1); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, child := range typed {
			if candidate := findDouyinStreamURL(child, depth+1); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}
