package bilibili

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestSubscriptionVideoMediaResolvesCIDBeforePlayURL(t *testing.T) {
	actions := newActions()
	actions.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"bvid": "BVfixture", "cid": 123, "duration": 60}}),
		testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"durl": []any{map[string]any{"url": "https://media.test/original.mp4"}}}}),
	}
	settings := plugin.NormalizeResolverSettings(plugin.ResolverSettings{}).Media
	plan, err := (&session{actions: actions}).ResolverMedia(context.Background(), plugin.Update{
		"id": "dynamic-id", "service": "video", "_resolver_video": map[string]any{"bvid": "BVfixture", "duration_text": "01:00"},
	}, settings)
	if err != nil || len(plan.Sources) != 1 || plan.Sources[0].Kind != "video" {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	if len(actions.HTTPRequests) != 2 {
		t.Fatalf("requests = %#v", actions.HTTPRequests)
	}
	query, _ := url.Parse(actions.HTTPRequests[1].URL)
	if query.Query().Get("cid") != "123" || query.Query().Get("bvid") != "BVfixture" {
		t.Fatalf("play query = %s", query)
	}
}

func TestSubscriptionLiveMediaUsesRoomIDInsteadOfTransitionID(t *testing.T) {
	actions := newActions()
	actions.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"durl": []any{map[string]any{"url": "https://media.test/live.flv"}}}})}
	plan, err := (&session{actions: actions}).ResolverMedia(t.Context(), plugin.Update{
		"id": "live-123-started-1700000000", "service": "live", "live_status": 1, "room_id": "456",
	}, plugin.ResolverMediaSettings{})
	if err != nil || len(plan.Sources) != 1 || plan.Sources[0].Kind != "live" {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	query, _ := url.Parse(actions.HTTPRequests[0].URL)
	if query.Query().Get("cid") != "456" {
		t.Fatalf("live room query = %s", query)
	}
}

func TestDynamicOriginalImagesAreNotLimitedByCardGrid(t *testing.T) {
	pics := make([]any, 12)
	for index := range pics {
		pics[index] = map[string]any{"src": fmt.Sprintf("http://i0.hdslb.com/original-%d.jpg", index)}
	}
	images := dynamicImages(map[string]any{"draw": map[string]any{"items": pics}}, "image_text")
	plan := plugin.ResolverImagePlan(plugin.Update{"images": images}, nil, "image")
	if len(plan.Sources) != len(pics) || plan.Sources[11].URLs[0] != "https://i0.hdslb.com/original-11.jpg" {
		t.Fatalf("original image count = %d", len(plan.Sources))
	}
}

func TestOpusHTTPOriginalImagesSurviveParsingAndMediaPlanning(t *testing.T) {
	pics := make([]any, 12)
	for index := range pics {
		pics[index] = map[string]any{"url": fmt.Sprintf("http://i0.hdslb.com/bfs/new_dyn/fixture-%d.png", index), "width": 1080, "height": 1920}
	}
	document := map[string]any{"code": 0, "data": map[string]any{"item": map[string]any{
		"id_str": "12345", "modules": []any{
			map[string]any{"module_type": "MODULE_TYPE_AUTHOR", "module_author": map[string]any{"mid": "456", "name": "图文作者"}},
			map[string]any{"module_type": "MODULE_TYPE_CONTENT", "module_content": map[string]any{"paragraphs": []any{
				map[string]any{"para_type": 2, "pic": map[string]any{"pics": pics}},
			}}},
		},
	}}}
	update, err := previewDynamicUpdate(time.UTC, document, "https://www.bilibili.com/opus/12345")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&session{actions: newActions()}).ResolverMedia(t.Context(), update, plugin.ResolverMediaSettings{})
	if err != nil || len(plan.Sources) != len(pics) {
		t.Fatalf("opus media = %#v, %v", plan, err)
	}
	for index, source := range plan.Sources {
		want := fmt.Sprintf("https://i0.hdslb.com/bfs/new_dyn/fixture-%d.png", index)
		if source.Kind != "image" || len(source.URLs) != 1 || source.URLs[0] != want {
			t.Fatalf("image %d = %#v", index, source)
		}
	}
}

func TestChooseBilibiliDashVideoHonorsCodecAtSelectedHeight(t *testing.T) {
	candidates := []any{
		map[string]any{"height": 720, "codecid": 13, "bandwidth": 1_000_000, "base_url": "https://media.test/av1"},
		map[string]any{"height": 720, "codecid": 7, "bandwidth": 1_000_000, "base_url": "https://media.test/avc"},
		map[string]any{"height": 480, "codecid": 7, "bandwidth": 500_000, "base_url": "https://media.test/480"},
	}
	selected := chooseBilibiliDashVideo(candidates, 720, 60, plugin.ResolverMediaSettings{VideoCodec: "avc"}, false)
	if selected == nil || plugin.StringScalar(selected["base_url"]) != "https://media.test/avc" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestChooseBilibiliDashVideoLowersResolutionForSizeLimit(t *testing.T) {
	candidates := []any{
		map[string]any{"height": 1080, "codecid": 7, "bandwidth": 8_000_000, "base_url": "https://media.test/1080"},
		map[string]any{"height": 480, "codecid": 7, "bandwidth": 1_000_000, "base_url": "https://media.test/480"},
	}
	settings := plugin.ResolverMediaSettings{
		VideoCodec: "auto", BilibiliSmartResolution: true, BilibiliFileSizeLimitMB: 70, BilibiliMinResolution: 360,
	}
	selected := chooseBilibiliDashVideo(candidates, 1080, 480, settings, false)
	if selected == nil || plugin.IntScalar(selected["height"]) != 480 {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestBilibiliResolverURLListKeepsPrimaryAndFallbacks(t *testing.T) {
	urls := bilibiliResolverURLList(map[string]any{
		"base_url":   "https://media.test/primary",
		"backup_url": []any{"https://media.test/backup-one", "https://media.test/backup-two"},
	})
	if len(urls) != 3 || urls[0] != "https://media.test/primary" || urls[2] != "https://media.test/backup-two" {
		t.Fatalf("urls = %#v", urls)
	}
}

func TestBilibiliVideoPlanAcceptsPortraitResolution(t *testing.T) {
	document := map[string]any{"data": map[string]any{"dash": map[string]any{
		"video": []any{
			map[string]any{"width": 720, "height": 1280, "codecid": 7, "base_url": "https://media.test/720"},
			map[string]any{"width": 360, "height": 640, "codecid": 7, "base_url": "https://media.test/360"},
			map[string]any{"width": 480, "height": 852, "codecid": 7, "base_url": "https://media.test/480"},
		},
		"audio": []any{map[string]any{"base_url": "https://media.test/audio"}},
	}}}
	plan, err := bilibiliResolverVideoPlan(document, plugin.Update{"id": "portrait"}, nil, 31, 480, plugin.ResolverMediaSettings{VideoCodec: "auto"}, false)
	if err != nil || len(plan.Sources) != 1 {
		t.Fatalf("portrait video plan = %#v, %v", plan, err)
	}
	source := plan.Sources[0]
	if len(source.URLs) != 1 || source.URLs[0] != "https://media.test/480" || len(source.AudioURLs) != 1 {
		t.Fatalf("portrait video streams = %#v", source)
	}
}

func TestChooseBilibiliDashVideoUsesPortraitResolutionForSizeLimit(t *testing.T) {
	candidates := []any{
		map[string]any{"width": 1080, "height": 1920, "codecid": 7, "bandwidth": 8_000_000, "base_url": "https://media.test/1080"},
		map[string]any{"width": 480, "height": 852, "codecid": 7, "bandwidth": 1_000_000, "base_url": "https://media.test/480"},
	}
	settings := plugin.ResolverMediaSettings{VideoCodec: "auto", BilibiliSmartResolution: true, BilibiliFileSizeLimitMB: 70, BilibiliMinResolution: 360}
	selected := chooseBilibiliDashVideo(candidates, 1080, 480, settings, false)
	if plugin.StringScalar(selected["base_url"]) != "https://media.test/480" {
		t.Fatalf("selected = %#v", selected)
	}
}
