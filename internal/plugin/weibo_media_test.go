package plugin

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestInlineWeiboUpdateMediaUsesRefererAndCompactCandidate(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpRoutes = []fakeHTTPRoute{{
		path:   "/mw690/cover.jpg",
		result: avatarHTTPResult(),
	}}
	data := map[string]any{
		"media_items": []map[string]any{{
			"url": "https://wx2.sinaimg.cn/orj360/cover.jpg", "class": "media-item", "fallback": "assets/cover.svg",
		}},
	}

	resolved, failed := inlineWeiboUpdateMedia(context.Background(), fake, data, nil)

	if resolved != 1 || failed != 0 {
		t.Fatalf("media resolution = %d/%d, data=%#v", resolved, failed, data)
	}
	items := mapSliceValue(data["media_items"])
	if len(items) != 1 || !strings.HasPrefix(stringScalar(items[0]["url"]), "data:image/png;base64,") {
		t.Fatalf("media was not inlined: %#v", items)
	}
	if len(fake.httpRequests) != 1 || fake.httpRequests[0].URL != "https://wx2.sinaimg.cn/mw690/cover.jpg" {
		t.Fatalf("unexpected media requests: %#v", fake.httpRequests)
	}
	if fake.httpRequests[0].Headers["Referer"] != "https://weibo.com/" {
		t.Fatalf("weibo media referer = %#v", fake.httpRequests[0].Headers)
	}
}

func TestInlineWeiboUpdateMediaKeepsFailedRemoteItemsAsPlaceholders(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/orj360/broken.jpg", result: rayleabot.ActionResult{"status_code": 403}},
		{path: "/thumbnail/broken.jpg", result: rayleabot.ActionResult{"status_code": 403}},
	}
	data := map[string]any{
		"media_items": []map[string]any{{"url": "https://wx2.sinaimg.cn/orj360/broken.jpg"}},
	}

	resolved, failed := inlineWeiboUpdateMedia(context.Background(), fake, data, nil)

	items := mapSliceValue(data["media_items"])
	if resolved != 0 || failed != 1 || len(items) != 1 || stringScalar(items[0]["url"]) != "assets/grid.svg" {
		t.Fatalf("failed remote media did not use a placeholder: resolved=%d failed=%d data=%#v", resolved, failed, data)
	}
	if intScalar(data["image_count"]) != 1 || stringScalar(data["media_grid_class"]) != "media-grid--single" {
		t.Fatalf("media layout metadata was not repaired: %#v", data)
	}
}

func TestInlineWeiboUpdateMediaFallsBackToFitWholeBatch(t *testing.T) {
	fake := newFakePluginActions()
	large := weiboMediaHTTPResult(120 << 10)
	compact := weiboMediaHTTPResult(16 << 10)
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/orj360/one.jpg", result: large}, {path: "/thumbnail/one.jpg", result: compact},
		{path: "/orj360/two.jpg", result: large}, {path: "/thumbnail/two.jpg", result: compact},
		{path: "/orj360/three.jpg", result: large}, {path: "/thumbnail/three.jpg", result: compact},
	}
	data := map[string]any{"media_items": []map[string]any{
		{"url": "https://wx2.sinaimg.cn/orj360/one.jpg"},
		{"url": "https://wx2.sinaimg.cn/orj360/two.jpg"},
		{"url": "https://wx2.sinaimg.cn/orj360/three.jpg"},
	}}

	resolved, failed := inlineWeiboUpdateMedia(context.Background(), fake, data, nil)

	if resolved != 3 || failed != 0 || len(mapSliceValue(data["media_items"])) != 3 {
		t.Fatalf("whole media batch was not retained: resolved=%d failed=%d data=%#v", resolved, failed, data)
	}
	for _, name := range []string{"one", "two", "three"} {
		found := false
		for _, request := range fake.httpRequests {
			if strings.Contains(request.URL, "/thumbnail/"+name+".jpg") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("compact fallback was not requested for %s: %#v", name, fake.httpRequests)
		}
	}
}

func weiboMediaHTTPResult(size int) rayleabot.ActionResult {
	body := make([]byte, size)
	copy(body, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	return rayleabot.ActionResult{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}
}
