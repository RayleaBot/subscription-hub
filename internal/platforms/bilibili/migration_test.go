package bilibili

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestWBISigningMatchesPreMigrationVector(t *testing.T) {
	signed, err := signWBIURL(
		"https://api.bilibili.com/x/polymer/web-dynamic/v1/feed/all?type=all&page=1",
		"7cd084941338484aae1ad9425b84077c",
		"4932caff0ff746eab6f01bf08b70ac45",
		1780905600,
	)
	if err != nil {
		t.Fatalf("signWBIURL() error = %v", err)
	}
	if !strings.Contains(signed, "wts=1780905600") || !strings.Contains(signed, "w_rid=389c1304f65697bbde60fdd8f8a6f9b6") {
		t.Fatalf("unexpected signed URL: %s", signed)
	}
}

func TestBilibiliSearchUsesWBIEndpoint(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
		"code": 0,
		"data": map[string]any{"result": []any{
			map[string]any{"mid": 10001, "uname": "测试 UP", "fans": 128000, "upic": "//i0.hdslb.com/test.jpg", "usign": "测试简介"},
		}},
	})}
	users, err := searchBilibiliWithActions(context.Background(), fake, "测试 UP")
	if err != nil {
		t.Fatalf("searchBilibiliWithActions() error = %v", err)
	}
	if len(users) != 1 || users[0].UID != "10001" || users[0].Name != "测试 UP" || users[0].Fans != 128000 || users[0].Sign != "测试简介" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("HTTP requests = %d, want 1 cached signed search request", len(fake.HTTPRequests))
	}
	parsed, _ := url.Parse(fake.HTTPRequests[0].URL)
	if parsed.Path != "/x/web-interface/wbi/search/type" || parsed.Query().Get("w_rid") == "" {
		t.Fatalf("search URL did not use signed WBI endpoint: %s", fake.HTTPRequests[0].URL)
	}
	if !strings.Contains(fake.HTTPRequests[0].Headers["Cookie"], "SESSDATA=primary") {
		t.Fatalf("search request did not use configured account")
	}
}

func TestBilibiliSearchRotatesAccountsAfterRiskControl(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary", "backup")
	seedSourceState(fake, time.Now(), "", "primary", "backup")
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(412, map[string]any{"code": -412, "message": "request was banned"}),
		testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{"result": []any{
				map[string]any{"mid": 10001, "uname": "测试 UP", "fans": 128000},
			}},
		}),
	}
	users, err := searchBilibiliWithActions(context.Background(), fake, "测试 UP")
	if err != nil || len(users) != 1 {
		t.Fatalf("account rotation search users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[0].Headers["Cookie"], "primary") || !strings.Contains(fake.HTTPRequests[1].Headers["Cookie"], "backup") {
		t.Fatalf("account rotation requests = %#v", fake.HTTPRequests)
	}
}

func TestBilibiliUserLookupUsesWBIEndpoint(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/x/space/wbi/acc/info", Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{
				"mid": 123456, "name": "测试 UP", "face": "//i0.hdslb.com/face.jpg", "sign": "测试简介", "level": 6,
				"official": map[string]any{"type": 0, "title": "bilibili 知名UP主", "desc": "认证说明"},
			},
		})},
		{Path: "/x/relation/stat", Result: testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"follower": 128000}})},
		{Path: "/x/space/wbi/arc/search", Result: testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"page": map[string]any{"count": 233}}})},
	}
	user, err := readBilibiliUserWithActions(context.Background(), fake, "123456")
	if err != nil || user.UID != "123456" || user.Name != "测试 UP" || user.AvatarURL != "https://i0.hdslb.com/face.jpg" {
		t.Fatalf("WBI user lookup user=%#v, err=%v", user, err)
	}
	if user.Sign != "测试简介" || user.Fans != 128000 {
		t.Fatalf("WBI user lookup profile user=%#v", user)
	}
	if user.Level != 6 || user.Verify != "bilibili 知名UP主" || user.VerifyOrg || user.Videos != 233 {
		t.Fatalf("WBI user lookup detail fields user=%#v", user)
	}
	if len(fake.HTTPRequests) != 3 {
		t.Fatalf("user lookup requests = %#v", fake.HTTPRequests)
	}
	parsed, _ := url.Parse(fake.HTTPRequests[0].URL)
	if parsed.Path != "/x/space/wbi/acc/info" || parsed.Query().Get("mid") != "123456" || parsed.Query().Get("w_rid") == "" {
		t.Fatalf("user lookup did not use signed WBI endpoint: %s", fake.HTTPRequests[0].URL)
	}
	// 粉丝数与投稿数并发读取，顺序不固定。
	var sawFans, sawVideos bool
	for _, request := range fake.HTTPRequests[1:] {
		requestURL, _ := url.Parse(request.URL)
		switch requestURL.Path {
		case "/x/relation/stat":
			sawFans = requestURL.Query().Get("vmid") == "123456"
		case "/x/space/wbi/arc/search":
			sawVideos = requestURL.Query().Get("mid") == "123456" && requestURL.Query().Get("w_rid") != ""
		}
	}
	if !sawFans || !sawVideos {
		t.Fatalf("supplementary lookups missing: fans=%v videos=%v, requests=%#v", sawFans, sawVideos, fake.HTTPRequests)
	}
}

func TestBilibiliUserLookupToleratesFansFailure(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/x/space/wbi/acc/info", Result: testkit.HTTPJSON(200, map[string]any{
			"code": 0,
			"data": map[string]any{"mid": 123456, "name": "测试 UP"},
		})},
		{Path: "/x/relation/stat", Err: errors.New("network down")},
	}
	user, err := readBilibiliUserWithActions(context.Background(), fake, "123456")
	if err != nil || user.UID != "123456" || user.Fans != 0 || user.Videos != 0 {
		t.Fatalf("fans failure should be tolerated, user=%#v, err=%v", user, err)
	}
}

func TestBilibiliSourceRotatesAccountsAfterRiskControl(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary", "backup")
	now := time.Unix(1780905600, 0)
	seedSourceState(fake, now, "123456", "primary", "backup")
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(412, map[string]any{"code": -412, "message": "request was banned"}),
		testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"items": []any{}}}),
	}
	source := newBilibiliSource(fake)
	source.client.now = func() time.Time { return now }
	result := source.Poll(context.Background(), []plugin.Subscription{{
		ID: "bilibili-one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true,
	}})
	if !result.DynamicOK || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "风控") {
		t.Fatalf("unexpected source result: %#v", result)
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[0].Headers["Cookie"], "primary") || !strings.Contains(fake.HTTPRequests[1].Headers["Cookie"], "backup") {
		t.Fatalf("account rotation requests = %#v", fake.HTTPRequests)
	}
	if _, exists := fake.KV["source:bilibili:cooldown:dynamic:primary"]; !exists {
		t.Fatalf("risk-controlled account did not enter cooldown: %#v", fake.KV)
	}
	if len(fake.Logs) == 0 || fake.Logs[0].Level != "debug" || plugin.StringScalar(fake.Logs[0].Fields["kind"]) != "risk_control" {
		t.Fatalf("risk-control failure was not logged: %#v", fake.Logs)
	}
}

func TestBilibiliSourceRefreshesWBIKeysAfterSignatureRejection(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	now := time.Now()
	fake.KV["source:bilibili:follow:primary:123456"] = map[string]any{"checked_at": now.Unix(), "following": true}
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, fixtureNavDocument()),
		testkit.HTTPJSON(200, map[string]any{"code": -403, "message": "invalid w_rid"}),
		testkit.HTTPJSON(200, fixtureNavDocument()),
		dynamicFeedResult(),
	}
	source := newBilibiliSource(fake)
	result := source.Poll(context.Background(), []plugin.Subscription{{
		ID: "bilibili-one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true,
	}})
	if !result.DynamicOK || len(result.Errors) != 0 {
		t.Fatalf("signature refresh result = %#v", result)
	}
	if len(fake.HTTPRequests) != 4 || fake.HTTPRequests[0].URL != bilibiliNavURL || fake.HTTPRequests[2].URL != bilibiliNavURL {
		t.Fatalf("signature refresh requests = %#v", fake.HTTPRequests)
	}
}

func TestAccountFeedCollectsMultipleSubjectsInOneRequest(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	now := time.Now()
	seedSourceState(fake, now, "123456", "primary")
	fake.KV["source:bilibili:follow:primary:654321"] = map[string]any{"checked_at": now.Unix(), "following": true}
	second := videoDynamic("second", "第二位 UP 的视频", 1700000060)
	second["modules"].(map[string]any)["module_author"].(map[string]any)["mid"] = "654321"
	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(
		videoDynamic("first", "第一位 UP 的视频", 1700000000), second,
	)}
	result := newBilibiliSource(fake).Poll(context.Background(), []plugin.Subscription{
		{ID: "one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true},
		{ID: "two", Platform: "bilibili", UID: "654321", Services: []string{"video"}, Enabled: true},
	})
	if !result.DynamicOK || len(result.Updates) != 2 {
		t.Fatalf("multi-subject result = %#v", result)
	}
	if len(fake.HTTPRequests) != 1 || !strings.Contains(fake.HTTPRequests[0].URL, "/feed/all") {
		t.Fatalf("account feed requests = %#v", fake.HTTPRequests)
	}
}

func TestSubscriptionCheckBaselinesThenRendersNewDynamic(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	now := time.Now().Truncate(time.Second)
	seedSourceState(fake, now, "123456", "primary")
	current := plugin.Settings{Enabled: true, Subscriptions: []plugin.Subscription{{
		ID: "bilibili-123456-group-10000", Platform: "bilibili", UID: "123456", Name: "测试 UP",
		TargetType: "group", TargetID: "10000", Services: []string{"video"}, Enabled: true,
		Subscribers: []plugin.Subscriber{{ID: "42", Nickname: "订阅人"}},
	}}}
	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic("old", "已有视频", now.Add(-time.Minute).Unix()))}
	first := checkAt(t, context.Background(), fake, current, now)
	if plugin.IntScalar(first["sent"]) != 0 || !plugin.BoolScalar(fake.KV[dynamicSourceKey(current.Subscriptions[0])]) {
		t.Fatalf("first check did not establish baseline: result=%#v kv=%#v", first, fake.KV)
	}
	if len(fake.Renders) != 0 || len(fake.Messages) != 0 {
		t.Fatalf("baseline emitted historical content")
	}
	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(
		videoDynamic("new", "新视频", now.Add(time.Minute).Unix()),
		videoDynamic("old", "已有视频", now.Add(-time.Minute).Unix()),
	)}
	second := checkAt(t, context.Background(), fake, current, now.Add(2*time.Minute))
	if plugin.IntScalar(second["sent"]) != 1 || plugin.BoolScalar(second["degraded"]) {
		t.Fatalf("second check result = %#v", second)
	}
	if len(fake.Renders) != 1 || len(fake.Messages) != 1 {
		t.Fatalf("render/message counts = %d/%d", len(fake.Renders), len(fake.Messages))
	}
	if fake.Renders[0].Data["title"] != "新视频" || fake.Renders[0].Data["service"] != "视频" {
		t.Fatalf("rich render data was not restored: %#v", fake.Renders[0].Data)
	}
	if _, exists := fake.KV["seen:bilibili-123456-group-10000:video:new"]; !exists {
		t.Fatalf("new update was not marked seen")
	}
}

func TestSubscriptionCheckContinuesFanoutAfterTargetFailure(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	now := time.Now().Truncate(time.Second)
	seedSourceState(fake, now, "123456", "primary")
	subscriptions := []plugin.Subscription{
		{ID: "target-one", Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "10000", Services: []string{"video"}, Enabled: true},
		{ID: "target-two", Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "20000", Services: []string{"video"}, Enabled: true},
	}
	for _, item := range subscriptions {
		fake.KV[dynamicSourceKey(item)] = true
	}
	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic("new", "新视频", now.Add(-time.Minute).Unix()))}
	fake.MessageErrors = []error{errors.New("target unavailable"), nil}
	result := checkAt(t, context.Background(), fake, plugin.Settings{Enabled: true, Subscriptions: subscriptions}, now)
	if plugin.IntScalar(result["sent"]) != 1 || !plugin.BoolScalar(result["degraded"]) || len(fake.Messages) != 2 {
		t.Fatalf("fanout result=%#v messages=%#v", result, fake.Messages)
	}
	if _, exists := fake.KV["seen:target-one:video:new"]; exists {
		t.Fatalf("failed target was incorrectly marked seen")
	}
	if _, exists := fake.KV["seen:target-two:video:new"]; !exists {
		t.Fatalf("successful target was not marked seen")
	}
}

func TestSubscriptionCheckExpiresFailedDynamicDelivery(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	now := time.Now().Truncate(time.Second)
	seedSourceState(fake, now, "123456", "primary")
	item := plugin.Subscription{
		ID: "stale-target", Platform: "bilibili", UID: "123456", Name: "测试 UP",
		TargetType: "private", TargetID: "20000", Services: []string{"video"}, Enabled: true,
	}
	fake.KV[dynamicSourceKey(item)] = true
	current := plugin.Settings{Enabled: true, DeliveryMaxAgeMinutes: 5, Subscriptions: []plugin.Subscription{item}}
	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic(
		"delayed", "发送失败后延迟恢复的视频", now.Add(-time.Minute).Unix(),
	))}
	fake.MessageErrors = []error{errors.New("adapter unavailable")}

	first := checkAt(t, context.Background(), fake, current, now)
	if plugin.IntScalar(first["sent"]) != 0 || !plugin.BoolScalar(first["degraded"]) || len(fake.Messages) != 1 {
		t.Fatalf("initial failed delivery result=%#v messages=%#v", first, fake.Messages)
	}
	if _, exists := fake.KV["seen:stale-target:video:delayed"]; exists {
		t.Fatalf("fresh failed delivery was incorrectly marked seen")
	}

	fake.HTTPResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic(
		"delayed", "发送失败后延迟恢复的视频", now.Add(-time.Minute).Unix(),
	))}
	second := checkAt(t, context.Background(), fake, current, now.Add(5*time.Minute))

	if plugin.IntScalar(second["sent"]) != 0 || plugin.BoolScalar(second["degraded"]) {
		t.Fatalf("expired delivery result = %#v", second)
	}
	if len(fake.Renders) != 1 || len(fake.Messages) != 1 {
		t.Fatalf("expired delivery was retried: renders=%d messages=%d", len(fake.Renders), len(fake.Messages))
	}
	if _, exists := fake.KV["seen:stale-target:video:delayed"]; !exists {
		t.Fatalf("expired delivery was not marked seen: %#v", fake.KV)
	}
	if len(fake.Logs) == 0 || fake.Logs[len(fake.Logs)-1].Fields["platform"] != "bilibili" || fake.Logs[len(fake.Logs)-1].Fields["update_id"] != "delayed" {
		t.Fatalf("expired delivery skip was not logged: %#v", fake.Logs)
	}
}

func TestLiveSourceBaselinesThenEmitsTransition(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureAccounts("primary")
	liveDocument := func(status int) rayleabot.ActionResult {
		return testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{
			"123456": map[string]any{"uid": 123456, "uname": "测试 UP", "room_id": 777, "title": "直播标题", "live_status": status, "live_time": 1700000000},
		}})
	}
	source := newBilibiliSource(fake)
	fake.HTTPResponses = []rayleabot.ActionResult{liveDocument(1)}
	first := source.Poll(context.Background(), []plugin.Subscription{{
		ID: "live-one", Platform: "bilibili", UID: "123456", Services: []string{"live"}, Enabled: true,
	}})
	if !first.LiveOK || len(first.Updates) != 0 {
		t.Fatalf("live baseline result = %#v", first)
	}
	fake.HTTPResponses = []rayleabot.ActionResult{liveDocument(0)}
	second := source.Poll(context.Background(), []plugin.Subscription{{
		ID: "live-one", Platform: "bilibili", UID: "123456", Services: []string{"live"}, Enabled: true,
	}})
	if len(second.Updates) != 1 || plugin.StringScalar(second.Updates[0]["live_event"]) != "ended" {
		t.Fatalf("live transition result = %#v", second)
	}
}

func TestDynamicNormalizationKeepsRichOpusContent(t *testing.T) {
	document := map[string]any{"data": map[string]any{"items": []any{map[string]any{
		"id_str": "100000000000000002", "type": "DYNAMIC_TYPE_DRAW",
		"modules": map[string]any{
			"module_author": map[string]any{"mid": "123456", "name": "测试 UP", "pub_ts": 1700000000},
			"module_dynamic": map[string]any{
				"topic": map[string]any{"name": "测试活动 2026"},
				"major": map[string]any{"type": "MAJOR_TYPE_OPUS", "opus": map[string]any{
					"summary": map[string]any{"text": "#测试活动 2026#\n正文[打call]", "rich_text_nodes": []any{
						map[string]any{"type": "RICH_TEXT_NODE_TYPE_TOPIC", "text": "#测试活动 2026#"},
						map[string]any{"type": "RICH_TEXT_NODE_TYPE_TEXT", "text": "\n正文"},
						map[string]any{"type": "RICH_TEXT_NODE_TYPE_TEXT", "text": "[打call]", "emoji": map[string]any{"icon_url": "//i0.hdslb.com/call.png", "text": "[打call]"}},
					}},
					"pics": []any{map[string]any{"url": "//i0.hdslb.com/pic.jpg", "width": 900, "height": 1600}},
				}},
			},
		},
	}}}}
	updates := dynamicUpdates(document)
	if len(updates) != 1 {
		t.Fatalf("updates = %#v", updates)
	}
	update := updates[0]
	if plugin.StringScalar(plugin.NestedValue(update, "topic", "name")) != "测试活动 2026" || !strings.Contains(plugin.StringScalar(update["summary_html"]), "rich-text-emoji") {
		t.Fatalf("rich content was lost: %#v", update)
	}
	if images := plugin.ImageMaps(update["images"], 9); len(images) != 1 || plugin.StringScalar(images[0]["url"]) != "https://i0.hdslb.com/pic.jpg" {
		t.Fatalf("image normalization was lost: %#v", update["images"])
	}
}

func TestBilibiliDiagnosticsRedactCredentials(t *testing.T) {
	response := rayleabot.ActionResult{
		"status_code": 412,
		"body_text":   `{"code":-412,"message":"blocked SESSDATA=private; bili_jct=csrf; Authorization: Bearer token-value"}`,
	}
	document := plugin.DecodeHTTPDocument(response)
	message := bilibiliDiagnosticText(response, document)
	if strings.Contains(message, "private") || strings.Contains(message, "csrf") || strings.Contains(message, "token-value") {
		t.Fatalf("diagnostic leaked credentials: %s", message)
	}
	for _, marker := range []string{"SESSDATA=[已隐藏]", "bili_jct=[已隐藏]", "Authorization: [已隐藏]"} {
		if !strings.Contains(message, marker) {
			t.Fatalf("diagnostic missing redaction %q: %s", marker, message)
		}
	}
	quoted := plugin.DiagnosticExcerpt(`{"cookie":"SESSDATA=quoted-secret", "authorization":"Bearer quoted-token", "refresh_token":"refresh-secret"}`, 500)
	for _, secret := range []string{"quoted-secret", "quoted-token", "refresh-secret"} {
		if strings.Contains(quoted, secret) {
			t.Fatalf("quoted diagnostic leaked %q: %s", secret, quoted)
		}
	}
}

func TestRenderDataTruncatesRichHTMLWithoutBreakingMarkup(t *testing.T) {
	body := strings.Repeat("长正文", 150)
	data := buildBilibiliRenderData(plugin.Subscription{UID: "123456", Name: "测试 UP"}, map[string]any{
		"title": "测试动态", "service": "image_text", "summary": body,
		"summary_html": `<span class="rich-text-topic">#话题#</span><span>` + body + `</span>`,
	})
	rendered := plugin.StringScalar(data["summary_html"])
	if !strings.Contains(rendered, "...") || !strings.HasSuffix(rendered, "</span>") {
		t.Fatalf("rich HTML was not safely truncated: %s", rendered)
	}
	if visible := plugin.HTMLVisibleLength(rendered); visible > 423 {
		t.Fatalf("rich HTML visible length = %d, want <= 423", visible)
	}
}

func TestPreviewURLParserCoversHistoricalLinkKinds(t *testing.T) {
	tests := map[string]string{
		"https://www.bilibili.com/video/BV1abc123":   "video",
		"https://www.bilibili.com/video/av123456":    "video",
		"https://www.bilibili.com/bangumi/play/ep99": "bangumi_ep",
		"https://www.bilibili.com/bangumi/play/ss88": "bangumi_season",
		"https://www.bilibili.com/read/cv77":         "article",
		"https://www.bilibili.com/read/mobile?id=66": "article",
		"https://www.bilibili.com/opus/1000000001":   "opus",
		"https://t.bilibili.com/1000000002":          "dynamic",
		"https://live.bilibili.com/777":              "live",
	}
	for rawURL, kind := range tests {
		ref := parseBilibiliPreviewURL(rawURL)
		if ref == nil || ref.Kind != kind {
			t.Fatalf("parseBilibiliPreviewURL(%q) = %#v, want %s", rawURL, ref, kind)
		}
	}
}

func fixtureAccounts(ids ...string) rayleabot.ActionResult {
	accounts := make([]any, 0, len(ids))
	for _, id := range ids {
		accounts = append(accounts, map[string]any{
			"account_id": id,
			"cookie":     map[string]any{"value": "SESSDATA=" + id + "; bili_jct=fixture;"},
			"profile":    map[string]any{"uid": "90000", "nickname": "fixture"},
		})
	}
	return rayleabot.ActionResult{"accounts": accounts}
}

func fixtureNavDocument() map[string]any {
	return map[string]any{
		"code": 0,
		"data": map[string]any{"wbi_img": map[string]any{
			"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
		}},
	}
}

func seedSourceState(fake *testkit.Actions, now time.Time, uid string, accountIDs ...string) {
	for _, accountID := range accountIDs {
		fake.KV["source:bilibili:wbi:"+accountID] = map[string]any{
			"img_key": "7cd084941338484aae1ad9425b84077c", "sub_key": "4932caff0ff746eab6f01bf08b70ac45", "expires_at": now.Add(time.Hour).Unix(),
		}
		fake.KV["source:bilibili:follow:"+accountID+":"+uid] = map[string]any{"checked_at": now.Unix(), "following": true}
	}
}

func dynamicFeedResult(items ...map[string]any) rayleabot.ActionResult {
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, item)
	}
	return testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{"items": values}})
}

func videoDynamic(id, title string, timestamp int64) map[string]any {
	return map[string]any{
		"id_str": id, "type": "DYNAMIC_TYPE_AV",
		"basic": map[string]any{"jump_url": "https://www.bilibili.com/video/BVFixture"},
		"modules": map[string]any{
			"module_author": map[string]any{"mid": "123456", "name": "测试 UP", "pub_ts": timestamp},
			"module_dynamic": map[string]any{
				"desc": map[string]any{"text": "视频简介"},
				"major": map[string]any{"type": "MAJOR_TYPE_ARCHIVE", "archive": map[string]any{
					"title": title, "cover": "//i0.hdslb.com/video.jpg", "duration": 201,
				}},
			},
		},
	}
}
