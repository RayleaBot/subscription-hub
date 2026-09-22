package douyin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestPrepareResolverCardInlinesCoverBeforeRendering(t *testing.T) {
	fake := newActions()
	fake.HTTPDefault = testkit.AvatarHTTPResult()
	item := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAfixture", Name: "测试用户"}
	update := normalizeDouyinAweme(time.UTC, douyinVideoAweme("video", item.UID, item.Name, "测试作品", 1700000000))
	card := (&session{actions: fake}).UpdateCard(item, update)

	prepared := (&session{actions: fake}).PrepareResolverCard(t.Context(), card)
	if len(prepared.Resources) != 0 {
		t.Fatalf("resolver card kept remote render resources: %#v", prepared.Resources)
	}
	items := plugin.MapSliceValue(prepared.Data["media_items"])
	if got := plugin.StringScalar(items[0]["url"]); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("resolver cover was not inlined: %q", got)
	}
	for _, item := range items {
		if plugin.StringScalar(item["resource_id"]) != "" {
			t.Fatalf("resolver cover kept resource id: %#v", item)
		}
	}
	if len(fake.HTTPRequests) != 1 || fake.HTTPRequests[0].TimeoutSeconds != douyinResolverCardImageTimeout {
		t.Fatalf("resolver cover request = %#v", fake.HTTPRequests)
	}
	if fake.HTTPRequests[0].URL != "https://p3-pc.douyinpic.com/fixture-published-cover.jpeg" {
		t.Fatalf("resolver fetched a video frame instead of the published cover: %s", fake.HTTPRequests[0].URL)
	}
}

func TestPrepareResolverCardFallsBackWhenCoverTimesOut(t *testing.T) {
	fake := newActions()
	fake.HTTPErrors = []error{context.DeadlineExceeded}
	card := resolverCardFixture()

	prepared := (&session{actions: fake}).PrepareResolverCard(t.Context(), card)
	if len(prepared.Resources) != 0 {
		t.Fatalf("timed out cover kept remote render resources: %#v", prepared.Resources)
	}
	items := plugin.MapSliceValue(prepared.Data["media_items"])
	if got := plugin.StringScalar(items[0]["url"]); got != "assets/cover.svg" {
		t.Fatalf("timed out cover did not use template fallback: %q", got)
	}
	if plugin.StringScalar(items[0]["resource_id"]) != "" {
		t.Fatalf("timed out cover kept resource id: %#v", items[0])
	}
}

func resolverCardFixture() plugin.CardRequest {
	return plugin.CardRequest{
		Data: map[string]any{"media_items": []map[string]any{
			{"url": "assets/cover.svg", "fallback": "assets/cover.svg", "resource_id": "douyin-media-0"},
			{"url": "assets/grid.svg", "fallback": "assets/grid.svg", "resource_id": "douyin-media-1"},
			{"url": "assets/grid.svg", "fallback": "assets/grid.svg", "resource_id": "douyin-media-2"},
			{"url": "assets/grid.svg", "fallback": "assets/grid.svg", "resource_id": "douyin-media-3"},
		}},
		Resources: []plugin.RenderResource{{
			ID: "douyin-media-0", URL: "https://p3-pc.douyinpic.com/aweme/cover.jpeg", Referer: douyinRenderResourceReferer,
		}},
	}
}
