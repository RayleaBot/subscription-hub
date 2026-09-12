package bilibili

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestMatchBilibiliUserByQuery(t *testing.T) {
	users := []bilibiliUser{{UID: "100", Name: "洛天依Official"}, {UID: "200", Name: "洛天依"}}
	if matched := matchBilibiliUserByQuery(users, "200"); matched == nil || matched.UID != "200" {
		t.Fatalf("uid input should match by uid: %#v", matched)
	}
	if matched := matchBilibiliUserByQuery(users, "洛天依"); matched == nil || matched.UID != "200" {
		t.Fatalf("exact nickname should win over prefix match: %#v", matched)
	}
	if matched := matchBilibiliUserByQuery(users, " 洛天依 "); matched == nil || matched.UID != "200" {
		t.Fatalf("trimmed exact nickname should match: %#v", matched)
	}
	if matched := matchBilibiliUserByQuery(users, "洛天"); matched != nil {
		t.Fatalf("partial nickname should not match: %#v", matched)
	}
	if matched := matchBilibiliUserByQuery(users, "300"); matched != nil {
		t.Fatalf("unknown uid should not match: %#v", matched)
	}
}

func TestBilibiliSearchUsesAnOverallTimeout(t *testing.T) {
	fake := newActions()
	fake.SeedAccounts("bilibili", fixtureAccounts("primary"))
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{{
		Path: "/x/web-interface/wbi/search/type",
		Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{map[string]any{"mid": 100, "uname": "Raylea"}}},
		}),
	}}
	actions := &testkit.DeadlineActions{Actions: fake}
	startedAt := time.Now()

	users, err := searchBilibiliWithActions(context.Background(), actions, "Raylea")
	if err != nil || len(users) != 1 {
		t.Fatalf("searchBilibiliWithActions() = %#v, %v", users, err)
	}
	if actions.Deadline.IsZero() {
		t.Fatal("Bilibili search request did not receive an overall deadline")
	}
	if actions.Deadline.After(startedAt.Add(bilibiliSearchTotalTimeout + time.Second)) {
		t.Fatalf("Bilibili search deadline exceeded its total budget: %s", actions.Deadline.Sub(startedAt))
	}
}

func TestAddBilibiliSubscriptionByNicknameSubscribesExactMatch(t *testing.T) {
	fake := newActions()
	fake.SeedAccounts("bilibili", fixtureAccounts("primary"))
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/x/web-interface/wbi/search/type", Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{
				map[string]any{"mid": 100, "uname": "洛天依Official"},
				map[string]any{"mid": 200, "uname": "洛天依", "upic": "//i0.hdslb.com/face.jpg"},
			}},
		})},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("洛天依"), "bilibili")
	if !outcome.Changed || outcome.Action != "subscribed" {
		t.Fatalf("exact nickname was not subscribed: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "200" || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscribed the wrong account: %#v", current.Subscriptions)
	}
}

func TestAddBilibiliSubscriptionByNicknameReturnsCandidatesWithoutExactMatch(t *testing.T) {
	fake := newActions()
	fake.SeedAccounts("bilibili", fixtureAccounts("primary"))
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/x/web-interface/wbi/search/type", Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{
				map[string]any{"mid": 100, "uname": "洛天依Official"},
				map[string]any{"mid": 300, "uname": "洛天依Channel"},
			}},
		})},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("洛天依"), "bilibili")
	if outcome.Changed || outcome.Action != "candidates" || len(outcome.Candidates) != 2 || outcome.CandidatesQuery != "洛天依" {
		t.Fatalf("ambiguous nickname should return candidates: %#v", outcome)
	}
	if !strings.Contains(outcome.Message, "洛天依") || !strings.Contains(outcome.Message, "完全一致") || !strings.Contains(outcome.Message, "更准确的昵称或 UID 重新订阅") {
		t.Fatalf("candidate message lacks guidance: %q", outcome.Message)
	}
	if len(current.Subscriptions) != 0 {
		t.Fatalf("ambiguous nickname must not persist a subscription: %#v", current.Subscriptions)
	}
}

func TestAddBilibiliSubscriptionByUIDKeepsDirectRead(t *testing.T) {
	fake := newActions()
	fake.SeedAccounts("bilibili", fixtureAccounts("primary"))
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/x/space/wbi/acc/info", Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0, "data": map[string]any{"mid": 123456, "name": "测试 UP"},
		})},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("123456"), "bilibili")
	if !outcome.Changed || outcome.Action != "subscribed" || current.Subscriptions[0].UID != "123456" || current.Subscriptions[0].Name != "测试 UP" {
		t.Fatalf("uid subscription regressed: %#v / %#v", outcome, current.Subscriptions)
	}
}
