package weibo

import "testing"

func TestWeiboResolverVideoURLsPrefersHighDefinitionAndDeduplicates(t *testing.T) {
	urls := weiboResolverVideoURLs(map[string]any{
		"page_info": map[string]any{"media_info": map[string]any{
			"stream_url_hd": "https://media.test/hd.mp4",
			"stream_url":    "https://media.test/sd.mp4",
			"playback_list": []any{map[string]any{"play_info": map[string]any{"url": "https://media.test/hd.mp4"}}},
		}},
	})
	if len(urls) != 2 || urls[0] != "https://media.test/hd.mp4" || urls[1] != "https://media.test/sd.mp4" {
		t.Fatalf("urls = %#v", urls)
	}
}

func TestWeiboResolverVideoURLsUsesStableQualityOrder(t *testing.T) {
	urls := weiboResolverVideoURLs(map[string]any{
		"page_info": map[string]any{
			"urls": map[string]any{
				"mp4_ld_mp4":   "https://media.test/ld.mp4",
				"mp4_720p_mp4": "https://media.test/720p.mp4",
				"mp4_hd_mp4":   "https://media.test/hd.mp4",
			},
			"media_info": map[string]any{"stream_url": "https://media.test/stream.mp4"},
		},
	})
	want := []string{"https://media.test/720p.mp4", "https://media.test/hd.mp4", "https://media.test/ld.mp4", "https://media.test/stream.mp4"}
	if len(urls) != len(want) {
		t.Fatalf("urls = %#v", urls)
	}
	for index := range want {
		if urls[index] != want[index] {
			t.Fatalf("urls = %#v, want %#v", urls, want)
		}
	}
}
