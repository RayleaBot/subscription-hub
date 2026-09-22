package bilibili

import (
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

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
