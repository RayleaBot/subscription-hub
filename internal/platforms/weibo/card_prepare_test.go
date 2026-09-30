package weibo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestPrepareWeiboCardKeepsAllMainAndRepostImagesBeyondInlineBudget(t *testing.T) {
	fake := testkit.NewActions()
	payload := make([]byte, 224<<10)
	copy(payload, []byte("\x89PNG\r\n\x1a\n"))
	fake.HTTPDefault = testkit.AvatarHTTPResult()
	fake.HTTPDefault["body_base64"] = base64.StdEncoding.EncodeToString(payload)
	mainItems, repostItems := make([]map[string]any, 9), make([]map[string]any, 6)
	for index := range mainItems {
		mainItems[index] = map[string]any{"url": fmt.Sprintf("https://wx1.sinaimg.cn/large/main-%d.png", index), "fallback": "assets/grid.svg"}
	}
	for index := range repostItems {
		repostItems[index] = map[string]any{"url": fmt.Sprintf("https://wx2.sinaimg.cn/large/repost-%d.png", index), "fallback": "assets/grid.svg"}
	}
	data := map[string]any{"media_items": mainItems, "original": map[string]any{"media_items": repostItems}}
	card := plugin.CardRequest{Data: data, Resources: prepareWeiboUpdateResources(data)}
	prepared := (&session{actions: fake}).PrepareUpdateCard(t.Context(), card)
	resources := make(map[string]bool)
	for _, resource := range prepared.Resources {
		resources[resource.ID] = true
	}
	if _, err := plugin.RenderSubscriptionCardImageWithResources(t.Context(), fake, "weibo-update", prepared.Data, prepared.Resources, "fixture", nil); err != nil {
		t.Fatal(err)
	}
	if len(fake.ResourceRenders) != 1 || len(fake.ResourceRenders[0].Resources) != 15 {
		t.Fatalf("render lost resources: %#v", fake.ResourceRenders)
	}
	encoded, err := json.Marshal(fake.ResourceRenders[0])
	if err != nil || len(encoded) >= 64<<10 {
		t.Fatalf("image bytes leaked into render JSON: %d, %v", len(encoded), err)
	}
	for index, item := range weiboCardMediaItems(prepared.Data) {
		if !strings.HasPrefix(plugin.StringScalar(item["url"]), "data:image/") && !resources[plugin.StringScalar(item["resource_id"])] {
			t.Errorf("downloadable image %d was replaced by fallback", index)
		}
	}
}

func TestPrepareWeiboCardPreservesMainAndRepostResourceBindings(t *testing.T) {
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
	if len(prepared.Resources) != 4 {
		t.Fatalf("prepared card lost remote resources: %#v", prepared.Resources)
	}
	items := weiboCardMediaItems(prepared.Data)
	for index, item := range items {
		if plugin.StringScalar(item["resource_id"]) != prepared.Resources[index].ID {
			t.Fatalf("item %d lost resource id: %#v", index, item)
		}
		url := plugin.StringScalar(item["url"])
		if url != "assets/grid.svg" {
			t.Fatalf("item %d lost its load-failure fallback: %q", index, url)
		}
		if index == 0 && (plugin.IntScalar(item["width"]) != 1080 || plugin.IntScalar(item["height"]) != 1800) {
			t.Fatalf("item %d lost intrinsic size: %#v", index, item)
		}
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatal("plugin fetched media instead of retaining host resources")
	}
	for _, resource := range prepared.Resources {
		if !strings.Contains(resource.URL, "/mw690/") || resource.Referer != "https://weibo.com/" || len(resource.FallbackURLs) != 4 {
			t.Fatalf("invalid preview candidates: %#v", resource)
		}
	}

}

func TestPrepareWeiboCardRetainsFallbackWhileHostResolvesMedia(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPDefault = testkit.HTTPJSON(504, map[string]any{})
	data := map[string]any{"media_items": []map[string]any{{
		"url": "https://wx1.sinaimg.cn/orj360/main.jpg", "fallback": "assets/grid.svg",
	}}}
	card := plugin.CardRequest{Data: data, Resources: prepareWeiboUpdateResources(data)}

	prepared := (&session{actions: fake}).PrepareUpdateCard(t.Context(), card)
	if len(prepared.Resources) != 1 {
		t.Fatalf("fallback card lost remote resources: %#v", prepared.Resources)
	}
	item := weiboCardMediaItems(prepared.Data)[0]
	if plugin.StringScalar(item["url"]) != "assets/grid.svg" || plugin.StringScalar(item["resource_id"]) != prepared.Resources[0].ID {
		t.Fatalf("fallback item = %#v", item)
	}
}

func weiboCardMediaItems(data map[string]any) []map[string]any {
	items := append([]map[string]any(nil), plugin.MapSliceValue(data["media_items"])...)
	if original := plugin.MapValue(data["original"]); original != nil {
		items = append(items, plugin.MapSliceValue(original["media_items"])...)
	}
	return items
}
