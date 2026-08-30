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
