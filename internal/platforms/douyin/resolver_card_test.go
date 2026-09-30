package douyin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestResolverCardKeepsPublishedCoverAsRenderResource(t *testing.T) {
	fake := newActions()
	item := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAfixture", Name: "测试用户"}
	update := normalizeDouyinAweme(time.UTC, douyinVideoAweme("video", item.UID, item.Name, "测试作品", 1700000000))
	source := &session{actions: fake}
	card := source.UpdateCard(item, update)
	if preparer, ok := any(source).(plugin.ResolverCardPreparingSession); ok {
		card = preparer.PrepareResolverCard(t.Context(), card)
	}
	items := plugin.MapSliceValue(card.Data["media_items"])
	if len(card.Resources) != 1 || len(items) != 1 || items[0]["resource_id"] != card.Resources[0].ID {
		t.Fatalf("cover lost its resource binding: %#v", card)
	}
	if card.Resources[0].URL != "https://p3-pc.douyinpic.com/fixture-published-cover.jpeg" || card.Resources[0].Referer != douyinRenderResourceReferer {
		t.Fatalf("published cover or referer changed: %#v", card.Resources)
	}
	if len(fake.HTTPRequests) != 0 || items[0]["url"] != "assets/cover.svg" {
		t.Fatal("cover must keep a local fallback while host fetches the resource")
	}
}

func TestResolverCardKeepsEveryResourceRegardlessOfInlineByteLimits(t *testing.T) {
	for _, test := range []struct {
		name        string
		count, size int
	}{
		{"combined budget", 3, 224 << 10},
		{"all nine images", 9, 8 << 10},
		{"large cover", 1, 300 << 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := testkit.NewActions()
			payload := make([]byte, test.size)
			copy(payload, []byte("\x89PNG\r\n\x1a\n"))
			fake.HTTPDefault = testkit.AvatarHTTPResult()
			fake.HTTPDefault["body_base64"] = base64.StdEncoding.EncodeToString(payload)
			images := make([]map[string]any, test.count)
			for index := range images {
				images[index] = map[string]any{"url": fmt.Sprintf("https://p3-pc.douyinpic.com/image-%d.jpeg", index), "candidates": []any{fmt.Sprintf("https://p6-pc.douyinpic.com/image-%d.jpeg", index)}}
			}
			source := &session{actions: fake}
			card := source.UpdateCard(plugin.Subscription{Platform: "douyin", UID: "fixture"}, plugin.Update{"service": "image_text", "images": images})
			if preparer, ok := any(source).(plugin.ResolverCardPreparingSession); ok {
				card = preparer.PrepareResolverCard(t.Context(), card)
			}
			if _, err := plugin.RenderSubscriptionCardImageWithResources(t.Context(), fake, "douyin-resolver", card.Data, card.Resources, "fixture", nil); err != nil {
				t.Fatal(err)
			}
			if len(fake.ResourceRenders) != 1 || len(fake.ResourceRenders[0].Resources) != test.count {
				t.Fatalf("render request lost images: %#v", fake.ResourceRenders)
			}
			for index, item := range plugin.MapSliceValue(card.Data["media_items"]) {
				resource := card.Resources[index]
				if item["resource_id"] != resource.ID || len(resource.FallbackURLs) != 1 {
					t.Fatalf("resource binding or mirror lost: %#v, %#v", item, resource)
				}
			}
			encoded, err := json.Marshal(fake.ResourceRenders[0])
			if err != nil || len(encoded) >= 64<<10 {
				t.Fatalf("image data leaked into render frame: %d, %v", len(encoded), err)
			}
		})
	}
}
