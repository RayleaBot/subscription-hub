package weibo

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

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

func TestAddWeiboSubscriptionByNicknameSubscribesExactMatch(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", QueryContains: "100103type", Result: testkit.HTTPJSON(200, weiboSearchDocument(
			map[string]any{"id": 6000000001, "screen_name": "Vsinger_洛天依"},
			map[string]any{"id": 6000000002, "screen_name": "洛天依", "profile_image_url": "https://tvax2.sinaimg.cn/face.jpg"},
		))},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("洛天依"), "weibo")
	if !outcome.Changed || outcome.Action != "subscribed" {
		t.Fatalf("exact nickname was not subscribed: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "6000000002" || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscribed the wrong account: %#v", current.Subscriptions)
	}
	if outcome.User == nil || outcome.User.AvatarURL == "" {
		t.Fatalf("resolved user was not carried into the outcome: %#v", outcome.User)
	}
}

func TestAddWeiboSubscriptionByNicknameReturnsCandidatesWithoutExactMatch(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", QueryContains: "100103type", Result: testkit.HTTPJSON(200, weiboSearchDocument(
			map[string]any{"id": 6000000001, "screen_name": "Vsinger_洛天依"},
		))},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("洛天依"), "weibo")
	if outcome.Changed || outcome.Action != "candidates" || len(outcome.Candidates) != 1 {
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
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", QueryContains: "100505", Err: errors.New("network down")},
	}
	current := plugin.Settings{}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, testkit.SubscriptionEvent("6000000001"), "weibo")
	if !outcome.Changed || len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "6000000001" {
		t.Fatalf("explicit uid lost its offline fallback: %#v / %#v", outcome, current.Subscriptions)
	}
}

func fixtureWeiboAccounts(ids ...string) rayleabot.ActionResult {
	accounts := make([]any, 0, len(ids))
	for _, id := range ids {
		accounts = append(accounts, map[string]any{
			"account_id": id,
			"cookie":     map[string]any{"value": "SUB=" + id + "; X-CSRF-TOKEN=csrf-" + id + ";"},
			"profile":    map[string]any{"uid": "80000", "nickname": "fixture"},
		})
	}
	return rayleabot.ActionResult{"accounts": accounts}
}

func weiboSearchDocument(users ...map[string]any) map[string]any {
	group := make([]any, 0, len(users))
	for _, user := range users {
		group = append(group, map[string]any{"card_type": 42, "user": user})
	}
	return map[string]any{"ok": 1, "data": map[string]any{"cards": []any{
		map[string]any{"card_type": 11, "card_group": group},
	}}}
}

func TestWeiboSearchParsesMobileResults(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, weiboSearchDocument(
		map[string]any{
			"id": 6000000001, "screen_name": "测试博主",
			"avatar_hd":         "https://wx2.sinaimg.cn/orj480/face.jpg",
			"profile_image_url": "//tva1.sinaimg.cn/crop.0.0.640.640.180/face.jpg",
			"followers_count":   12865000, "verified": true, "verified_type": 0, "verified_reason": "知名科技博主", "description": "测试简介",
		},
	))}
	users, err := searchWeiboWithActions(context.Background(), fake, "测试博主")
	if err != nil || len(users) != 1 {
		t.Fatalf("searchWeiboWithActions() users=%#v, err=%v", users, err)
	}
	user := users[0]
	if user.UID != "6000000001" || user.Name != "测试博主" || user.AvatarURL != "https://tva1.sinaimg.cn/crop.0.0.640.640.180/face.jpg" {
		t.Fatalf("search lost identity fields: %#v", user)
	}
	if user.FansText != "粉丝 1286.5万" || user.Verify != "知名科技博主" || user.VerifyOrg || user.Sign != "测试简介" {
		t.Fatalf("search lost profile fields: %#v", user)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("search requests = %#v", fake.HTTPRequests)
	}
	request := fake.HTTPRequests[0]
	parsed, _ := url.Parse(request.URL)
	if parsed.Host != "m.weibo.cn" || parsed.Path != "/api/container/getIndex" || !strings.Contains(request.URL, "100103type%3D3") || parsed.Query().Get("page_type") != "searchall" {
		t.Fatalf("search did not use the mobile user-search endpoint: %s", request.URL)
	}
	if !strings.Contains(request.Headers["Cookie"], "SUB=primary") || request.Headers["x-csrf-token"] != "csrf-primary" {
		t.Fatalf("search did not use the configured account: %#v", request.Headers)
	}
}

func TestWeiboSearchFallsBackToSecondSearchType(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"cards": []any{}}}),
		testkit.HTTPJSON(200, weiboSearchDocument(map[string]any{"id": 6000000002, "screen_name": "二号博主"})),
	}
	users, err := searchWeiboWithActions(context.Background(), fake, "二号博主")
	if err != nil || len(users) != 1 || users[0].UID != "6000000002" {
		t.Fatalf("type fallback users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[0].URL, "100103type%3D3") || !strings.Contains(fake.HTTPRequests[1].URL, "100103type%3D1") {
		t.Fatalf("search did not retry type 1 after empty type 3: %#v", fake.HTTPRequests)
	}
}

func TestWeiboSearchRotatesAccountsAfterAuthFailure(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary", "backup"))
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(403, map[string]any{"ok": 0, "msg": "forbidden"}),
		testkit.HTTPJSON(200, weiboSearchDocument(map[string]any{"id": 6000000003, "screen_name": "三号博主"})),
	}
	users, err := searchWeiboWithActions(context.Background(), fake, "三号博主")
	if err != nil || len(users) != 1 {
		t.Fatalf("account rotation users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[0].Headers["Cookie"], "SUB=primary") || !strings.Contains(fake.HTTPRequests[1].Headers["Cookie"], "SUB=backup") {
		t.Fatalf("account rotation requests = %#v", fake.HTTPRequests)
	}
}

func TestWeiboSearchFallsBackToWebSearch(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	page := `<div class="card card-user-b"><a href="https://weibo.com/u/6000000009" nick-name="网页博主">网页博主</a><img src="https://tva2.sinaimg.cn/crop.0.0.100.100.50/face.jpg"/></div>`
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"cards": []any{}}}),
		testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"cards": []any{}}}),
		{"status_code": 200, "body_text": page, "headers": map[string]any{}},
	}
	users, err := searchWeiboWithActions(context.Background(), fake, "网页博主")
	if err != nil || len(users) != 1 {
		t.Fatalf("web fallback users=%#v, err=%v", users, err)
	}
	if users[0].UID != "6000000009" || users[0].Name != "网页博主" || users[0].AvatarURL != "https://tva2.sinaimg.cn/crop.0.0.100.100.50/face.jpg" {
		t.Fatalf("web fallback lost profile fields: %#v", users[0])
	}
	if len(fake.HTTPRequests) != 3 {
		t.Fatalf("web fallback requests = %#v", fake.HTTPRequests)
	}
	parsed, _ := url.Parse(fake.HTTPRequests[2].URL)
	if parsed.Host != "s.weibo.com" || parsed.Path != "/user" || parsed.Query().Get("q") != "网页博主" {
		t.Fatalf("web fallback did not use s.weibo.com: %s", fake.HTTPRequests[2].URL)
	}
}

func TestWeiboSearchReportsEmptyResults(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"cards": []any{}}}),
		testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"cards": []any{}}}),
		{"status_code": 200, "body_text": "<html><body>暂无结果</body></html>", "headers": map[string]any{}},
	}
	_, err := searchWeiboWithActions(context.Background(), fake, "不存在")
	if err == nil || !strings.Contains(err.Error(), "没有搜索到微博博主：不存在") {
		t.Fatalf("empty search did not report a not-found error: %v", err)
	}
}

func TestWeiboSearchRequiresAccount(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", rayleabot.ActionResult{"accounts": []any{}})
	_, err := searchWeiboWithActions(context.Background(), fake, "测试博主")
	if err == nil || !strings.Contains(err.Error(), "没有可用的微博账号 CK") {
		t.Fatalf("missing account did not fail with guidance: %v", err)
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("search without accounts should not send requests: %#v", fake.HTTPRequests)
	}
}

func TestWeiboSearchDeduplicatesUsers(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	duplicate := map[string]any{"id": 6000000001, "screen_name": "测试博主"}
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, weiboSearchDocument(duplicate, duplicate))}
	users, err := searchWeiboWithActions(context.Background(), fake, "测试博主")
	if err != nil || len(users) != 1 {
		t.Fatalf("duplicate users were not merged: users=%#v, err=%v", users, err)
	}
}

func TestReadWeiboUserParsesDetailProfile(t *testing.T) {
	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
		"ok": 1,
		"data": map[string]any{"userInfo": map[string]any{
			"id": 6000000001, "screen_name": "测试博主", "profile_image_url": "https://tva1.sinaimg.cn/face.jpg",
			"followers_count": 2330000, "verified": true, "verified_type": 2, "verified_reason": "示例机构官方微博", "description": "机构简介",
		}},
	})}
	user, err := readWeiboUserWithActions(context.Background(), fake, "6000000001")
	if err != nil {
		t.Fatalf("readWeiboUserWithActions() error = %v", err)
	}
	if user.UID != "6000000001" || user.Name != "测试博主" || user.AvatarURL != "https://tva1.sinaimg.cn/face.jpg" {
		t.Fatalf("detail lost identity fields: %#v", user)
	}
	if user.FansText != "粉丝 233万" || user.Verify != "示例机构官方微博" || !user.VerifyOrg || user.Sign != "机构简介" {
		t.Fatalf("detail lost profile fields: %#v", user)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("detail requests = %#v", fake.HTTPRequests)
	}
	parsed, _ := url.Parse(fake.HTTPRequests[0].URL)
	if parsed.Query().Get("containerid") != "1005056000000001" || parsed.Query().Get("type") != "uid" || parsed.Query().Get("value") != "6000000001" {
		t.Fatalf("detail did not use the mobile profile endpoint: %s", fake.HTTPRequests[0].URL)
	}
}

func TestResolveWeiboUsersRoutesUIDLinkAndNickname(t *testing.T) {
	newResolvedFake := func() *testkit.Actions {
		fake := testkit.NewActions()
		fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
		fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
			"ok": 1, "data": map[string]any{"userInfo": map[string]any{"id": 6000000001, "screen_name": "测试博主"}},
		})}
		return fake
	}
	for _, input := range []string{"6000000001", "https://weibo.com/u/6000000001", "https://m.weibo.cn/u/6000000001"} {
		fake := newResolvedFake()
		users, err := resolveWeiboUsersWithActions(context.Background(), fake, input)
		if err != nil || len(users) != 1 || users[0].UID != "6000000001" {
			t.Fatalf("resolveWeiboUsersWithActions(%q) users=%#v, err=%v", input, users, err)
		}
		if len(fake.HTTPRequests) != 1 || !strings.Contains(fake.HTTPRequests[0].URL, "containerid=1005056000000001") {
			t.Fatalf("resolveWeiboUsersWithActions(%q) did not read the detail profile: %#v", input, fake.HTTPRequests)
		}
	}

	fake := testkit.NewActions()
	fake.SeedAccounts("weibo", fixtureWeiboAccounts("primary"))
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, weiboSearchDocument(map[string]any{"id": 6000000002, "screen_name": "昵称博主"}))}
	users, err := resolveWeiboUsersWithActions(context.Background(), fake, "昵称博主")
	if err != nil || len(users) != 1 || users[0].UID != "6000000002" {
		t.Fatalf("nickname resolve users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 1 || !strings.Contains(fake.HTTPRequests[0].URL, "100103type%3D3") {
		t.Fatalf("nickname resolve did not use search: %#v", fake.HTTPRequests)
	}
}

func TestWeiboUIDFromInput(t *testing.T) {
	for input, want := range map[string]string{
		"12345":                           "12345",
		"https://weibo.com/u/12345":       "12345",
		"https://m.weibo.cn/u/12345":      "12345",
		"https://weibo.com/profile/12345": "12345",
		"https://weibo.com/12345":         "12345",
		"看看 https://weibo.com/u/12345 这个": "12345",
		"普通昵称":                            "",
		"https://example.com/u/12345":     "",
	} {
		if got := weiboUIDFromInput(input); got != want {
			t.Fatalf("weiboUIDFromInput(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWeiboSearchAvatarFromHTMLDecodesSignedQuery(t *testing.T) {
	raw := `<img src="https://tvax2.sinaimg.cn/large/fixture.jpg?KID=imgbed,tva&amp;Expires=1780905600&amp;ssig=fixture-signature" />`
	avatarURL := weiboSearchAvatarFromHTML(raw)
	parsed, err := url.Parse(avatarURL)
	if err != nil {
		t.Fatalf("parse decoded avatar URL: %v", err)
	}
	if parsed.Query().Get("KID") != "imgbed,tva" || parsed.Query().Get("Expires") != "1780905600" || parsed.Query().Get("ssig") != "fixture-signature" {
		t.Fatalf("signed avatar query was not decoded: %q", avatarURL)
	}
	if strings.Contains(parsed.RawQuery, "amp;") {
		t.Fatalf("signed avatar kept HTML entity artifacts: %q", avatarURL)
	}
}

func TestWeiboDiagnosticsNeverIncludeUpstreamBody(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 403,
		"body_text":   `{"ok":0,"msg":"blocked SUB=private-value; SUBP=masked-value"}`,
	}}
	client := newWeiboClient(fake)
	_, err := client.requestJSON(context.Background(), weiboUserFeedURL("6000000001", ""), weiboAccount{ID: "primary", Cookie: "SUB=fixture;"}, weiboMobileReferer)
	if err == nil {
		t.Fatal("requestJSON error = nil")
	}
	message := err.Error() + " " + friendlyWeiboSourceError("微博检查失败", err)
	for _, secret := range []string{"private-value", "masked-value", "SUB=", "SUBP="} {
		if strings.Contains(message, secret) {
			t.Fatalf("weibo diagnostic leaked %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "HTTP 403") || !strings.Contains(message, "CK 已失效") {
		t.Fatalf("weibo diagnostic = %q", message)
	}
}
