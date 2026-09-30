package bilibili

import (
	"encoding/json"
	"fmt"
	"net/url"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestBilibiliCardResourcesCoverMainAndRepostImages(t *testing.T) {
	mainImages, repostImages := make([]map[string]any, 9), make([]map[string]any, 6)
	for index := range mainImages {
		mainImages[index] = map[string]any{"url": fmt.Sprintf("http://i0.hdslb.com/bfs/new_dyn/main-%d.png", index), "width": 1080, "height": 1920}
	}
	for index := range repostImages {
		repostImages[index] = map[string]any{"url": fmt.Sprintf("https://i1.hdslb.com/bfs/new_dyn/repost-%d.png", index)}
	}
	fake := testkit.NewActions()
	card := (&session{actions: fake}).UpdateCard(plugin.Subscription{Platform: "bilibili", UID: "10001"}, plugin.Update{
		"service": "repost", "images": mainImages,
		"original": map[string]any{"service": "image_text", "images": repostImages},
	})
	items := append(plugin.MapSliceValue(card.Data["media_items"]), plugin.MapSliceValue(plugin.NestedValue(card.Data, "original", "media_items"))...)
	if len(items) != 15 || len(card.Resources) != 15 {
		t.Fatalf("incomplete card resources: %d, %d", len(items), len(card.Resources))
	}
	for index, item := range items {
		resource := card.Resources[index]
		if item["resource_id"] != resource.ID || item["url"] != "assets/grid.svg" || resource.Referer != bilibiliRenderResourceReferer || len(resource.FallbackURLs) != 2 {
			t.Fatalf("image %d lost binding, fallback or mirrors: %#v, %#v", index, item, resource)
		}
		if index == 0 && (plugin.IntScalar(item["width"]) != 1080 || plugin.IntScalar(item["height"]) != 1920) {
			t.Fatalf("image dimensions changed: %#v", item)
		}
	}
	for _, templateID := range []string{"bilibili-update", "bilibili-resolver"} {
		if _, err := plugin.RenderSubscriptionCardImageWithResources(t.Context(), fake, templateID, card.Data, card.Resources, "fixture", nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.ResourceRenders) != 2 || len(fake.ResourceRenders[0].Resources) != 15 || len(fake.ResourceRenders[1].Resources) != 15 {
		t.Fatal("render action dropped media resources")
	}
	encoded, err := json.Marshal(fake.ResourceRenders)
	if err != nil || len(encoded) > 64<<10 {
		t.Fatalf("unexpected render payload: %d, %v", len(encoded), err)
	}
}

func TestBilibiliMediaMirrorsPreserveFileAndParameters(t *testing.T) {
	source := "https://i1.hdslb.com/bfs/archive/fixture.png@640w.webp?param=fixture"
	candidates := bilibiliMediaCandidateURLs(source)
	if len(candidates) != 3 || candidates[0] != source {
		t.Fatalf("candidates = %#v", candidates)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Path != "/bfs/archive/fixture.png@640w.webp" || parsed.RawQuery != "param=fixture" || seen[parsed.Host] {
			t.Fatalf("invalid mirror: %s", candidate)
		}
		seen[parsed.Host] = true
	}
}

func TestBilibiliResourcesKeepLocalAssetsAndRejectMalformedRemoteURLs(t *testing.T) {
	items := []map[string]any{
		{"url": "assets/cover.svg"},
		{"url": "https://user:fixture@i0.hdslb.com/private.png", "fallback": "assets/grid.svg"},
		{"url": "https://i0.hdslb.com/image.png#fragment", "fallback": "assets/grid.svg"},
	}
	resources := prepareBilibiliUpdateResources(map[string]any{"media_items": items})
	if len(resources) != 0 || items[0]["url"] != "assets/cover.svg" || items[1]["url"] != "assets/grid.svg" || items[2]["url"] != "assets/grid.svg" {
		t.Fatalf("unexpected fallback handling: %#v", items)
	}
}
