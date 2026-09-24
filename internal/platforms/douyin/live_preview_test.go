package douyin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

const fixtureDouyinRoomID = "7688348105741306660"

func douyinLiveRoomFixture(status int) map[string]any {
	return map[string]any{
		"id": 7688348105741307000, "idStr": fixtureDouyinRoomID, "status": status,
		"title": "测试直播", "startTime": 1700000000, "userCount": 123,
		"owner":     map[string]any{"secUid": "MS4wLjABAAAAfixture", "nickname": "测试主播", "webRid": "123456", "avatarThumb": map[string]any{"urlList": []any{"https://p3-pc.douyinpic.com/avatar.jpeg"}}},
		"cover":     map[string]any{"urlList": []any{"https://p3-pc.douyinpic.com/live.jpeg"}},
		"streamUrl": map[string]any{"flvPullUrl": map[string]any{"FULL_HD1": "https://live.test/hd.flv", "SD1": "https://live.test/sd.flv"}},
	}
}

func douyinRSCPage(chunks ...string) string {
	var body strings.Builder
	for _, chunk := range chunks {
		encoded, _ := json.Marshal([]any{1, chunk})
		body.WriteString("<script>self.__rsc_f.push(" + string(encoded) + ")</script>")
	}
	return body.String()
}

func douyinLiveRSCRecord(room map[string]any) string {
	encoded, _ := json.Marshal([]any{"$", "$L7", nil, map[string]any{"data": map[string]any{"room": room}}})
	return "5:" + string(encoded) + "\n"
}

func TestDouyinLivePreviewReadsFragmentedFlightData(t *testing.T) {
	record := douyinLiveRSCRecord(douyinLiveRoomFixture(2))
	text := `{"analytics":"中文文本，包含 6:[1]"}`
	page := douyinRSCPage("1:I[\"module\"]\n8:T"+fmt.Sprintf("%x", len(text))+",", text, record[:37], record[37:])
	live := douyinLivePreviewFromPage(page, &douyinPreviewRef{Kind: "live_reflow", ID: fixtureDouyinRoomID})
	if live == nil || live["room_id"] != fixtureDouyinRoomID || live["web_rid"] != "123456" {
		t.Fatalf("live room identity = %#v", live)
	}
	update := normalizeDouyinLive(time.UTC, live, "")
	if plugin.IntScalar(update["live_status"]) != 1 || plugin.IntScalar(update["pub_ts"]) != 1700000000 || plugin.NestedValue(update, "author", "uid") != "MS4wLjABAAAAfixture" {
		t.Fatalf("live update = %#v", update)
	}
	images := plugin.ImageMaps(update["images"], 9)
	if len(images) != 1 || images[0]["url"] != "https://p3-pc.douyinpic.com/live.jpeg" || douyinResolverLiveURL(live) != "https://live.test/hd.flv" {
		t.Fatalf("live assets = %#v", live)
	}
	if got := douyinLivePreviewFromPage(page, &douyinPreviewRef{Kind: "live_reflow", ID: "999"}); got != nil {
		t.Fatalf("unrelated room was used: %#v", got)
	}
	for _, test := range []struct {
		uid  string
		want bool
	}{
		{uid: "MS4wLjABAAAAfixture", want: true},
		{uid: "MS4wLjABAAAAanother", want: false},
	} {
		ref := &douyinPreviewRef{Kind: "live_reflow", ID: "999", URL: "https://webcast.amemv.com/douyin/webcast/reflow/999?sec_user_id=" + test.uid}
		if got := douyinLivePreviewFromPage(page, ref); (got != nil) != test.want {
			t.Fatalf("replacement room with owner %s = %#v", test.uid, got)
		}
	}
}

func TestDouyinLivePreviewKeepsEndedRoomWithoutRecording(t *testing.T) {
	page := douyinRSCPage(douyinLiveRSCRecord(douyinLiveRoomFixture(4)))
	live := douyinLivePreviewFromPage(page, &douyinPreviewRef{Kind: "live", ID: "123456"})
	update := normalizeDouyinLive(time.UTC, live, "")
	if update == nil || plugin.IntScalar(update["live_status"]) != 0 {
		t.Fatalf("ended live update = %#v", update)
	}
	plan, err := (&session{actions: newActions()}).ResolverMedia(t.Context(), update, plugin.ResolverMediaSettings{})
	if err != nil || len(plan.Sources) != 0 {
		t.Fatalf("ended room triggered recording: %#v, %v", plan, err)
	}
}

func TestFetchDouyinLiveShortLinkUsesPublicRoomWithoutAccounts(t *testing.T) {
	fake := newActions()
	roomURL := "https://webcast.amemv.com/douyin/webcast/reflow/" + fixtureDouyinRoomID
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/fixture-live/", Result: rayleabot.ActionResult{"status_code": 302, "headers": map[string]any{"Location": roomURL}}},
		{Path: "/douyin/webcast/reflow/" + fixtureDouyinRoomID, Result: rayleabot.ActionResult{
			"status_code": 200, "body_text": douyinRSCPage(douyinLiveRSCRecord(douyinLiveRoomFixture(2))),
		}},
	}
	update, err := fetchDouyinPreview(t.Context(), fake, parseDouyinPreviewURL("https://v.douyin.com/fixture-live/"))
	if err != nil || plugin.StringScalar(update["service"]) != "live" || update["url"] != "https://live.douyin.com/123456" {
		t.Fatalf("live short link = %#v, %v", update, err)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("live short link fetched extra pages: %#v", testkit.RequestURLs(fake))
	}
}

func TestDouyinProfileLinksDoNotFallThroughToVideoIDExtraction(t *testing.T) {
	profile := "https://www.iesdouyin.com/share/user/MS4wLjABAAAAfixture"
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			fake := newActions()
			input := profile
			if short {
				input = "https://v.douyin.com/fixture-user/"
				fake.HTTPRoutes = []testkit.HTTPRoute{
					{Path: "/fixture-user/", Result: rayleabot.ActionResult{"status_code": 302, "headers": map[string]any{"Location": profile}}},
					{Path: "/share/user/MS4wLjABAAAAfixture", Result: rayleabot.ActionResult{"status_code": 200, "body_text": "<html>profile</html>"}},
				}
			}
			_, err := fetchDouyinPreview(t.Context(), fake, parseDouyinPreviewURL(input))
			if !errors.Is(err, errDouyinProfilePreview) {
				t.Fatalf("profile error = %v", err)
			}
			wantRequests := 0
			if short {
				wantRequests = 1
			}
			if len(fake.HTTPRequests) != wantRequests {
				t.Fatalf("profile reached work API: %#v", testkit.RequestURLs(fake))
			}
		})
	}
}

func TestParseDouyinLiveRoutes(t *testing.T) {
	for _, input := range []string{"https://www.douyin.com/live/123456", "https://www.douyin.com/root/live/123456"} {
		ref := parseDouyinPreviewURL(input)
		if ref == nil || ref.Kind != "live" || ref.URL != "https://live.douyin.com/123456" {
			t.Fatalf("live route %q = %#v", input, ref)
		}
	}
	ref := parseDouyinPreviewURL("https://webcast.amemv.com/douyin/webcast/reflow/" + fixtureDouyinRoomID)
	if ref == nil || ref.Kind != "live_reflow" || ref.ID != fixtureDouyinRoomID {
		t.Fatalf("live reflow = %#v", ref)
	}
}
