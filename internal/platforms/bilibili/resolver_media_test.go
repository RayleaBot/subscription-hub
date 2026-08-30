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
		"base_url": "https://media.test/primary",
		"backup_url": []any{"https://media.test/backup-one", "https://media.test/backup-two"},
	})
	if len(urls) != 3 || urls[0] != "https://media.test/primary" || urls[2] != "https://media.test/backup-two" {
		t.Fatalf("urls = %#v", urls)
	}
}
