package bilibili

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func (session *session) ResolverMedia(ctx context.Context, update plugin.Update, settings plugin.ResolverMediaSettings) (plugin.ResolverMediaPlan, error) {
	account := bilibiliAccount{}
	if accounts, err := readBilibiliAccounts(ctx, session.actions); err == nil && len(accounts) > 0 {
		account = accounts[0]
	}
	headers := bilibiliRequestHeaders(account.Cookie, "https://www.bilibili.com", plugin.StringScalar(update["url"]), false)
	service := plugin.StringScalar(update["service"])
	if service == "live" {
		if plugin.IntScalar(update["live_status"]) != 1 {
			return plugin.ResolverMediaPlan{}, nil
		}
		roomID := plugin.StringScalar(plugin.NestedValue(update, "_resolver_live", "room_id"))
		if roomID == "" {
			roomID = strings.TrimPrefix(plugin.StringScalar(update["id"]), "live-")
		}
		document, err := newBilibiliClient(session.actions).requestJSON(ctx, "GET", bilibiliLivePlayURL+"?"+url.Values{
			"cid": {roomID}, "platform": {"web"}, "quality": {"4"},
		}.Encode(), account, false, true, "", false)
		if err != nil {
			return plugin.ResolverMediaPlan{}, errors.New(friendlyBilibiliSourceError("Bilibili 直播流获取失败", err))
		}
		urls := bilibiliResolverDURL(plugin.NestedValue(document, "data", "durl"))
		if len(urls) == 0 {
			return plugin.ResolverMediaPlan{}, errors.New("没有获取到 Bilibili 直播流")
		}
		return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{Kind: "live", URLs: urls, Headers: headers, FileName: "bilibili-live-" + roomID + ".mp4"}}}, nil
	}

	if bangumi := plugin.MapValue(update["_resolver_bangumi"]); bangumi != nil {
		if !settings.BilibiliBangumiDirect {
			return plugin.ResolverMediaPlan{}, nil
		}
		episode := plugin.MapValue(bangumi["episode"])
		duration := bilibiliResolverDuration(update, episode)
		if duration > settings.BilibiliBangumiMaxSeconds {
			return plugin.ResolverMediaPlan{}, &plugin.ResolverMediaSkippedError{Reason: fmt.Sprintf("番剧时长 %d 秒，超过 %d 秒限制，不发送视频", duration, settings.BilibiliBangumiMaxSeconds)}
		}
		query := bilibiliResolverPlayQuery(episode, settings.BilibiliBangumiResolution)
		if epID := plugin.FirstText(episode["id"], episode["ep_id"]); epID != "" {
			query.Set("ep_id", epID)
		}
		document, err := newBilibiliClient(session.actions).requestJSON(ctx, "GET", bilibiliPGCPlayURL+"?"+query.Encode(), account, false, false, "", false)
		if err != nil {
			return plugin.ResolverMediaPlan{}, errors.New(friendlyBilibiliSourceError("Bilibili 番剧视频获取失败", err))
		}
		return bilibiliResolverVideoPlan(document, update, headers, duration, settings.BilibiliBangumiResolution, settings, true)
	}

	video := plugin.MapValue(update["_resolver_video"])
	if video == nil && service == "repost" {
		video = plugin.MapValue(plugin.NestedValue(update, "original", "_resolver_video"))
	}
	if video == nil {
		return plugin.ResolverImagePlan(update, headers, "bilibili-image"), nil
	}
	duration := bilibiliResolverDuration(update, video)
	if duration > settings.BilibiliMaxDurationSeconds {
		return plugin.ResolverMediaPlan{}, &plugin.ResolverMediaSkippedError{Reason: fmt.Sprintf("视频时长 %d 秒，超过 %d 秒限制，不发送视频", duration, settings.BilibiliMaxDurationSeconds)}
	}
	query := bilibiliResolverPlayQuery(video, settings.BilibiliResolution)
	document, err := newBilibiliClient(session.actions).requestJSON(ctx, "GET", bilibiliVideoPlayURL+"?"+query.Encode(), account, false, false, "", false)
	if err != nil {
		return plugin.ResolverMediaPlan{}, errors.New(friendlyBilibiliSourceError("Bilibili 视频获取失败", err))
	}
	return bilibiliResolverVideoPlan(document, update, headers, duration, settings.BilibiliResolution, settings, false)
}

func bilibiliResolverPlayQuery(video map[string]any, resolution int) url.Values {
	values := url.Values{
		"qn": {strconv.Itoa(bilibiliQualityNumber(resolution))}, "fnval": {"4048"}, "fnver": {"0"}, "fourk": {"1"},
	}
	if bvid := plugin.StringScalar(video["bvid"]); bvid != "" {
		values.Set("bvid", bvid)
	}
	if aid := plugin.FirstText(video["aid"], video["avid"]); aid != "" {
		values.Set("avid", aid)
	}
	if cid := plugin.StringScalar(video["cid"]); cid != "" {
		values.Set("cid", cid)
	}
	return values
}

func bilibiliQualityNumber(height int) int {
	switch {
	case height >= 2160:
		return 120
	case height >= 1080:
		return 80
	case height >= 720:
		return 64
	case height >= 480:
		return 32
	default:
		return 16
	}
}

func bilibiliResolverVideoPlan(document map[string]any, update plugin.Update, headers map[string]string, duration, targetHeight int, settings plugin.ResolverMediaSettings, bangumi bool) (plugin.ResolverMediaPlan, error) {
	data := plugin.MapValue(document["data"])
	if data == nil {
		data = plugin.MapValue(document["result"])
	}
	if data == nil {
		return plugin.ResolverMediaPlan{}, errors.New("Bilibili 视频地址响应格式不正确")
	}
	video := chooseBilibiliDashVideo(plugin.SliceValue(plugin.NestedValue(data, "dash", "video")), targetHeight, duration, settings, bangumi)
	if video != nil {
		videoURLs := bilibiliResolverURLList(video)
		audio := chooseBilibiliDashAudio(plugin.SliceValue(plugin.NestedValue(data, "dash", "audio")))
		audioURLs := bilibiliResolverURLList(audio)
		if len(videoURLs) == 0 {
			return plugin.ResolverMediaPlan{}, errors.New("没有获取到 Bilibili 视频地址")
		}
		return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{
			Kind: "video", URLs: videoURLs, AudioURLs: audioURLs, Headers: headers,
			FileName: "bilibili-" + plugin.FirstText(update["id"], "video") + ".mp4", Duration: duration,
		}}}, nil
	}
	if urls := bilibiliResolverDURL(data["durl"]); len(urls) > 0 {
		return plugin.ResolverMediaPlan{Sources: []plugin.ResolverMediaSource{{
			Kind: "video", URLs: urls, Headers: headers, FileName: "bilibili-" + plugin.FirstText(update["id"], "video") + ".mp4", Duration: duration,
		}}}, nil
	}
	return plugin.ResolverMediaPlan{}, errors.New("没有获取到 Bilibili 视频地址")
}

func bilibiliResolverDuration(update plugin.Update, raw map[string]any) int {
	duration := int(plugin.IntScalar(plugin.FirstNonNil(update["duration_seconds"], raw["duration"])))
	if duration > 100000 {
		duration /= 1000
	}
	return duration
}

func chooseBilibiliDashVideo(raw []any, targetHeight, duration int, settings plugin.ResolverMediaSettings, bangumi bool) map[string]any {
	candidates := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		candidate := plugin.MapValue(item)
		if candidate != nil {
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		leftHeight, rightHeight := plugin.IntScalar(candidates[left]["height"]), plugin.IntScalar(candidates[right]["height"])
		if leftHeight != rightHeight {
			return leftHeight > rightHeight
		}
		return bilibiliCodecRank(candidates[left], settings.VideoCodec) < bilibiliCodecRank(candidates[right], settings.VideoCodec)
	})
	limitMB := settings.BilibiliFileSizeLimitMB
	minHeight := settings.BilibiliMinResolution
	if bangumi {
		limitMB = 0
		minHeight = 0
	}
	var fallback map[string]any
	for _, candidate := range candidates {
		height := int(plugin.IntScalar(candidate["height"]))
		if height > targetHeight {
			continue
		}
		if fallback == nil {
			fallback = candidate
		}
		if settings.VideoCodec != "auto" && bilibiliCodecName(candidate) != settings.VideoCodec {
			continue
		}
		if settings.BilibiliSmartResolution && limitMB > 0 && duration > 0 && height >= minHeight {
			estimatedBytes := plugin.IntScalar(candidate["bandwidth"]) * int64(duration) / 8
			if estimatedBytes > int64(limitMB)<<20 {
				continue
			}
		}
		return candidate
	}
	return fallback
}

func chooseBilibiliDashAudio(raw []any) map[string]any {
	var selected map[string]any
	for _, item := range raw {
		candidate := plugin.MapValue(item)
		if candidate != nil && (selected == nil || plugin.IntScalar(candidate["bandwidth"]) > plugin.IntScalar(selected["bandwidth"])) {
			selected = candidate
		}
	}
	return selected
}

func bilibiliCodecName(candidate map[string]any) string {
	switch plugin.IntScalar(candidate["codecid"]) {
	case 13:
		return "av1"
	case 12:
		return "hevc"
	default:
		return "avc"
	}
}

func bilibiliCodecRank(candidate map[string]any, preferred string) int {
	codec := bilibiliCodecName(candidate)
	if preferred != "auto" {
		if codec == preferred {
			return 0
		}
		return 1
	}
	switch codec {
	case "av1":
		return 0
	case "hevc":
		return 1
	default:
		return 2
	}
}

func bilibiliResolverURLList(raw map[string]any) []string {
	if raw == nil {
		return nil
	}
	result := make([]string, 0, 4)
	for _, candidate := range append([]any{raw["base_url"], raw["baseUrl"]}, plugin.SliceValue(plugin.FirstNonNil(raw["backup_url"], raw["backupUrl"]))...) {
		if value := plugin.StringScalar(candidate); strings.HasPrefix(value, "http") {
			result = append(result, value)
		}
	}
	return result
}

func bilibiliResolverDURL(raw any) []string {
	result := make([]string, 0, 4)
	for _, item := range plugin.SliceValue(raw) {
		if value := plugin.StringScalar(plugin.MapValue(item)["url"]); strings.HasPrefix(value, "http") {
			result = append(result, value)
		}
	}
	return result
}
