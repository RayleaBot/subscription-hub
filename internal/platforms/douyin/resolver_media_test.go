package douyin

import (
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
	"testing"
)

func TestSubscriptionLiveMediaFetchesStreamAndRechecksEndedRoom(t *testing.T) {
	for _, status := range []int{2, 4} {
		actions := newActions()
		actions.HTTPRoutes = []testkit.HTTPRoute{{Path: "/123456", Result: rayleabot.ActionResult{
			"status_code": 200, "body_text": douyinRSCPage(douyinLiveRSCRecord(douyinLiveRoomFixture(status))),
		}}}
		plan, err := (&session{actions: actions}).ResolverMedia(t.Context(), plugin.Update{
			"service": "live", "live_status": 1, "url": "https://live.douyin.com/123456",
		}, plugin.ResolverMediaSettings{})
		if err != nil {
			t.Fatal(err)
		}
		if status == 4 {
			if len(plan.Sources) != 0 {
				t.Fatalf("ended room still records: %#v", plan)
			}
		} else if len(plan.Sources) != 1 || plan.Sources[0].Kind != "live" || plan.Sources[0].URLs[0] != "https://live.test/hd.flv" {
			t.Fatalf("subscription live plan = %#v", plan)
		}
	}
}

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
