package weibo

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func (session *session) ResolverMedia(ctx context.Context, update plugin.Update, _ plugin.ResolverMediaSettings) (plugin.ResolverMediaPlan, error) {
	headers := map[string]string{"Referer": weiboMobileReferer}
	if accounts, err := readWeiboAccounts(ctx, session.actions); err == nil && len(accounts) > 0 && accounts[0].Cookie != "" {
		headers["Cookie"] = accounts[0].Cookie
	}
	mblog := plugin.MapValue(update["_resolver_mblog"])
	if plugin.StringScalar(update["service"]) == "repost" {
		if original := plugin.MapValue(mblog["retweeted_status"]); weiboMblogHasVideo(original) {
			mblog = original
		}
	}
	if !weiboMblogHasVideo(mblog) {
		return plugin.ResolverImagePlan(update, headers, "weibo-image"), nil
	}
	urls := weiboResolverVideoURLs(mblog)
	if len(urls) == 0 {
		return plugin.ResolverMediaPlan{}, errors.New("没有获取到微博视频地址")
	}
	return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{
		Kind: "video", URLs: urls, Headers: headers, FileName: "weibo-" + plugin.StringScalar(update["id"]) + ".mp4",
	}}}, nil
}

func weiboResolverVideoURLs(mblog map[string]any) []string {
	page := plugin.MapValue(mblog["page_info"])
	media := plugin.MapValue(page["media_info"])
	result := make([]string, 0, 8)
	seen := map[string]bool{}
	add := func(value any) {
		candidate := strings.TrimSpace(plugin.StringScalar(value))
		if strings.HasPrefix(candidate, "//") {
			candidate = "https:" + candidate
		}
		if !strings.HasPrefix(candidate, "http") || seen[candidate] {
			return
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	urlMaps := []map[string]any{plugin.MapValue(page["urls"]), plugin.MapValue(media["urls"])}
	for _, key := range []string{"mp4_1080p_mp4", "mp4_720p_mp4", "mp4_hd_mp4", "mp4_ld_mp4", "mp4_sd_mp4"} {
		for _, object := range urlMaps {
			add(object[key])
		}
	}
	for _, key := range []string{"stream_url_hd", "mp4_hd_url", "stream_url", "mp4_sd_url", "video_url", "url"} {
		add(media[key])
		add(page[key])
	}
	for _, raw := range plugin.SliceValue(media["playback_list"]) {
		item := plugin.MapValue(raw)
		add(plugin.NestedValue(item, "play_info", "url"))
		add(item["url"])
	}
	for _, object := range urlMaps {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			add(object[key])
		}
	}
	return result
}
