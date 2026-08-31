package weibo

import (
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestPrepareWeiboCardInlinesBoundedMainAndRepostMedia(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPDefault = testkit.AvatarHTTPResult()
	data := map[string]any{
		"media_items": []map[string]any{
			{"url": "https://wx1.sinaimg.cn/orj360/main-1.jpg", "fallback": "assets/grid.svg", "width": 1080, "height": 1800},
			{"url": "https://wx2.sinaimg.cn/orj360/main-2.jpg", "fallback": "assets/grid.svg"},
			{"url": "https://wx3.sinaimg.cn/orj360/main-3.jpg", "fallback": "assets/grid.svg"},
		},
		"original": map[string]any{"media_items": []map[string]any{
			{"url": "https://wx4.sinaimg.cn/orj360/repost-1.jpg", "fallback": "assets/grid.svg"},
		}},
	}
	card := plugin.CardRequest{Data: data, Resources: prepareWeiboUpdateResources(data)}

	prepared := (&session{actions: fake}).PrepareResolverCard(t.Context(), card)
	if len(prepared.Resources) != 0 {
		t.Fatalf("prepared card kept remote resources: %#v", prepared.Resources)
	}
	items := weiboCardMediaItems(prepared.Data)
	for index, item := range items {
		if plugin.StringScalar(item["resource_id"]) != "" {
			t.Fatalf("item %d kept resource id: %#v", index, item)
		}
		url := plugin.StringScalar(item["url"])
		if index < weiboCardMediaMaxItems && !strings.HasPrefix(url, "data:image/png;base64,") {
			t.Fatalf("item %d was not inlined: %q", index, url)
		}
		if index == 0 && (plugin.IntScalar(item["width"]) != 1080 || plugin.IntScalar(item["height"]) != 1800) {
			t.Fatalf("item %d lost intrinsic size: %#v", index, item)
		}
	}
	if len(fake.HTTPRequests) != len(items) {
		t.Fatalf("media requests = %#v", testkit.RequestURLs(fake))
	}
	for _, request := range fake.HTTPRequests {
		if !strings.Contains(request.URL, "/mw690/") || request.TimeoutSeconds != weiboCardImageTimeout {
			t.Fatalf("unexpected media request: %#v", request)
		}
	}
}

func TestPrepareWeiboUpdateCardFallsBackWithoutRenderResources(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPDefault = testkit.HTTPJSON(504, map[string]any{})
	data := map[string]any{"media_items": []map[string]any{{
		"url": "https://wx1.sinaimg.cn/orj360/main.jpg", "fallback": "assets/grid.svg",
	}}}
	card := plugin.CardRequest{Data: data, Resources: prepareWeiboUpdateResources(data)}

	prepared := (&session{actions: fake}).PrepareUpdateCard(t.Context(), card)
	if len(prepared.Resources) != 0 {
		t.Fatalf("fallback card kept remote resources: %#v", prepared.Resources)
	}
	item := weiboCardMediaItems(prepared.Data)[0]
	if plugin.StringScalar(item["url"]) != "assets/grid.svg" || plugin.StringScalar(item["resource_id"]) != "" {
		t.Fatalf("fallback item = %#v", item)
	}
}
