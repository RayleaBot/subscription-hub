package douyin

import "testing"

func TestDouyinResolverVideoURLsBuildsWatermarkFreePlayURL(t *testing.T) {
	urls := douyinResolverVideoURLs(map[string]any{
		"play_addr": map[string]any{"uri": "video-id", "url_list": []any{"https://media.test/fallback.mp4"}},
	}, 1080)
	if len(urls) != 2 || urls[0] != "https://aweme.snssdk.com/aweme/v1/play/?video_id=video-id&ratio=1080p&line=0" {
		t.Fatalf("urls = %#v", urls)
	}
}

func TestDouyinResolverLiveURLUsesHighestAvailableStream(t *testing.T) {
	value := douyinResolverLiveURL(map[string]any{"room": map[string]any{
		"stream_url": map[string]any{"flv_pull_url": map[string]any{
			"FULL_HD1": "https://live.test/full.flv", "HD1": "https://live.test/hd.flv",
		}},
	}})
	if value != "https://live.test/full.flv" {
		t.Fatalf("live url = %q", value)
	}
}
