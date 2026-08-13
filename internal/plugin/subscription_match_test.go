package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
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

func TestMatchWeiboUserByQuery(t *testing.T) {
	users := []weiboUser{{UID: "6000000001", Name: "Vsinger_洛天依"}, {UID: "6000000002", Name: "洛天依"}}
	if matched := matchWeiboUserByQuery(users, "6000000001"); matched == nil || matched.UID != "6000000001" {
		t.Fatalf("uid input should match by uid: %#v", matched)
	}
	if matched := matchWeiboUserByQuery(users, "https://weibo.com/u/6000000002"); matched == nil || matched.UID != "6000000002" {
		t.Fatalf("profile link should match by uid: %#v", matched)
	}
	if matched := matchWeiboUserByQuery(users, "洛天依"); matched == nil || matched.UID != "6000000002" {
		t.Fatalf("exact nickname should match: %#v", matched)
	}
	if matched := matchWeiboUserByQuery(users, "vsinger_洛天依"); matched == nil || matched.UID != "6000000001" {
		t.Fatalf("exact nickname should be case-insensitive: %#v", matched)
	}
	if matched := matchWeiboUserByQuery(users, "洛"); matched != nil {
		t.Fatalf("partial nickname should not match: %#v", matched)
	}
}

func TestManagementResolutionRequiresAnExactMatch(t *testing.T) {
	bilibiliUsers := []bilibiliUser{{UID: "100", Name: "Raylea Official"}}
	bilibiliResult := bilibiliManagementResolution("Raylea", bilibiliUsers)
	if bilibiliResult["exact"] != false {
		t.Fatalf("single approximate Bilibili candidate was marked exact: %#v", bilibiliResult)
	}
	if _, exists := bilibiliResult["user"]; exists {
		t.Fatalf("single approximate Bilibili candidate was returned as the resolved user: %#v", bilibiliResult)
	}

	weiboUsers := []weiboUser{{UID: "6000000001", Name: "Raylea_Official"}}
	weiboResult := weiboManagementResolution("Raylea", weiboUsers)
	if weiboResult["exact"] != false {
		t.Fatalf("single approximate Weibo candidate was marked exact: %#v", weiboResult)
	}
	if _, exists := weiboResult["user"]; exists {
		t.Fatalf("single approximate Weibo candidate was returned as the resolved user: %#v", weiboResult)
	}
}

func TestManagementResolutionReturnsTheExactCandidate(t *testing.T) {
	bilibiliUsers := []bilibiliUser{{UID: "100", Name: "Raylea Official"}, {UID: "200", Name: "Raylea"}}
	bilibiliResult := bilibiliManagementResolution("raylea", bilibiliUsers)
	bilibiliUser, ok := bilibiliResult["user"].(bilibiliUser)
	if bilibiliResult["exact"] != true || !ok || bilibiliUser.UID != "200" {
		t.Fatalf("exact Bilibili candidate was not resolved: %#v", bilibiliResult)
	}

	weiboUsers := []weiboUser{{UID: "6000000001", Name: "Raylea_Official"}, {UID: "6000000002", Name: "Raylea"}}
	weiboResult := weiboManagementResolution("raylea", weiboUsers)
	weiboUser, ok := weiboResult["user"].(weiboUser)
	if weiboResult["exact"] != true || !ok || weiboUser.UID != "6000000002" {
		t.Fatalf("exact Weibo candidate was not resolved: %#v", weiboResult)
	}
}

type deadlineCapturingActions struct {
	*fakePluginActions
	deadline time.Time
}

func (actions *deadlineCapturingActions) HTTPRequest(ctx context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	if deadline, ok := ctx.Deadline(); ok {
		actions.deadline = deadline
	}
	return actions.fakePluginActions.HTTPRequest(ctx, request)
}

func TestBilibiliSearchUsesAnOverallTimeout(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpRoutes = []fakeHTTPRoute{{
		path: "/x/web-interface/wbi/search/type",
		result: httpJSONResult(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{map[string]any{"mid": 100, "uname": "Raylea"}}},
		}),
	}}
	actions := &deadlineCapturingActions{fakePluginActions: fake}
	startedAt := time.Now()

	users, err := searchBilibiliWithActions(context.Background(), actions, "Raylea")
	if err != nil || len(users) != 1 {
		t.Fatalf("searchBilibiliWithActions() = %#v, %v", users, err)
	}
	if actions.deadline.IsZero() {
		t.Fatal("Bilibili search request did not receive an overall deadline")
	}
	if actions.deadline.After(startedAt.Add(bilibiliSearchTotalTimeout + time.Second)) {
		t.Fatalf("Bilibili search deadline exceeded its total budget: %s", actions.deadline.Sub(startedAt))
	}
}

func newSubscriptionEvent(args ...string) *rayleabot.EventContext {
	return &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": args},
	}}
}

func TestAddBilibiliSubscriptionByNicknameSubscribesExactMatch(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/x/web-interface/wbi/search/type", result: httpJSONResult(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{
				map[string]any{"mid": 100, "uname": "洛天依Official"},
				map[string]any{"mid": 200, "uname": "洛天依", "upic": "//i0.hdslb.com/face.jpg"},
			}},
		})},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("洛天依"), "bilibili")
	if !outcome.Changed || outcome.Action != "subscribed" {
		t.Fatalf("exact nickname was not subscribed: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "200" || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscribed the wrong account: %#v", current.Subscriptions)
	}
}

func TestAddBilibiliSubscriptionByNicknameReturnsCandidatesWithoutExactMatch(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/x/web-interface/wbi/search/type", result: httpJSONResult(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{
				map[string]any{"mid": 100, "uname": "洛天依Official"},
				map[string]any{"mid": 300, "uname": "洛天依Channel"},
			}},
		})},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("洛天依"), "bilibili")
	if outcome.Changed || outcome.Action != "candidates" || len(outcome.BilibiliCandidates) != 2 || outcome.CandidatesQuery != "洛天依" {
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
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/x/space/wbi/acc/info", result: httpJSONResult(200, map[string]any{
			"code": 0, "data": map[string]any{"mid": 123456, "name": "测试 UP"},
		})},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("123456"), "bilibili")
	if !outcome.Changed || outcome.Action != "subscribed" || current.Subscriptions[0].UID != "123456" || current.Subscriptions[0].Name != "测试 UP" {
		t.Fatalf("uid subscription regressed: %#v / %#v", outcome, current.Subscriptions)
	}
}

func TestAddWeiboSubscriptionByNicknameSubscribesExactMatch(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/api/container/getIndex", queryContains: "100103type", result: httpJSONResult(200, weiboSearchDocument(
			map[string]any{"id": 6000000001, "screen_name": "Vsinger_洛天依"},
			map[string]any{"id": 6000000002, "screen_name": "洛天依", "profile_image_url": "https://tvax2.sinaimg.cn/face.jpg"},
		))},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("洛天依"), "weibo")
	if !outcome.Changed || outcome.Action != "subscribed" {
		t.Fatalf("exact nickname was not subscribed: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "6000000002" || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscribed the wrong account: %#v", current.Subscriptions)
	}
	if outcome.WeiboUser == nil || outcome.WeiboUser.AvatarURL == "" {
		t.Fatalf("resolved user was not carried into the outcome: %#v", outcome.WeiboUser)
	}
}

func TestAddWeiboSubscriptionByNicknameReturnsCandidatesWithoutExactMatch(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/api/container/getIndex", queryContains: "100103type", result: httpJSONResult(200, weiboSearchDocument(
			map[string]any{"id": 6000000001, "screen_name": "Vsinger_洛天依"},
		))},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("洛天依"), "weibo")
	if outcome.Changed || outcome.Action != "candidates" || len(outcome.WeiboCandidates) != 1 {
		t.Fatalf("ambiguous nickname should return candidates: %#v", outcome)
	}
	if !strings.Contains(outcome.Message, "完全一致") || !strings.Contains(outcome.Message, "更准确的昵称或 UID 重新订阅") {
		t.Fatalf("candidate message lacks guidance: %q", outcome.Message)
	}
	if len(current.Subscriptions) != 0 {
		t.Fatalf("ambiguous nickname must not persist a subscription: %#v", current.Subscriptions)
	}
}

func TestAddWeiboSubscriptionByUIDKeepsOfflineFallback(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/api/container/getIndex", queryContains: "100505", err: errors.New("network down")},
	}
	current := settings{}
	outcome := addSubscriptionWithActions(context.Background(), fake, &current, newSubscriptionEvent("6000000001"), "weibo")
	if !outcome.Changed || len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "6000000001" {
		t.Fatalf("explicit uid lost its offline fallback: %#v / %#v", outcome, current.Subscriptions)
	}
}
