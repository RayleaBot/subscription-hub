package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type fakePluginActions struct {
	accounts      rayleabot.ActionResult
	httpResponses []rayleabot.ActionResult
	httpErrors    []error
	httpRequests  []rayleabot.HTTPRequest
	kv            map[string]any
	renders       []rayleabot.RenderImageRequest
	messages      []rayleabot.MessageSendRequest
	messageErrors []error
	logs          []rayleabot.LoggerWriteRequest
}

func newFakePluginActions() *fakePluginActions {
	return &fakePluginActions{kv: map[string]any{}}
}

func (fake *fakePluginActions) HTTPRequest(_ context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	fake.httpRequests = append(fake.httpRequests, request)
	if len(fake.httpErrors) > 0 {
		err := fake.httpErrors[0]
		fake.httpErrors = fake.httpErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(fake.httpResponses) == 0 {
		return httpJSONResult(200, map[string]any{"code": 0, "data": map[string]any{}}), nil
	}
	result := fake.httpResponses[0]
	fake.httpResponses = fake.httpResponses[1:]
	return result, nil
}

func (fake *fakePluginActions) ThirdPartyAccountRead(context.Context, rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error) {
	return fake.accounts, nil
}

func (fake *fakePluginActions) KVGet(_ context.Context, key string) (rayleabot.ActionResult, error) {
	value, exists := fake.kv[key]
	if !exists {
		return rayleabot.ActionResult{"exists": false}, nil
	}
	return rayleabot.ActionResult{"exists": true, "value": value}, nil
}

func (fake *fakePluginActions) KVSet(_ context.Context, key string, value any) (rayleabot.ActionResult, error) {
	fake.kv[key] = value
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *fakePluginActions) KVDelete(_ context.Context, key string) (rayleabot.ActionResult, error) {
	delete(fake.kv, key)
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *fakePluginActions) LoggerWrite(_ context.Context, request rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error) {
	fake.logs = append(fake.logs, request)
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *fakePluginActions) RenderImage(_ context.Context, request rayleabot.RenderImageRequest) (rayleabot.ActionResult, error) {
	fake.renders = append(fake.renders, request)
	return rayleabot.ActionResult{"image_path": "plugin-test.png"}, nil
}

func (fake *fakePluginActions) MessageSend(_ context.Context, request rayleabot.MessageSendRequest) (rayleabot.ActionResult, error) {
	fake.messages = append(fake.messages, request)
	if len(fake.messageErrors) > 0 {
		err := fake.messageErrors[0]
		fake.messageErrors = fake.messageErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return rayleabot.ActionResult{"message_id": "fixture-message"}, nil
}

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
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpResponses = []rayleabot.ActionResult{httpJSONResult(200, map[string]any{
		"code": 0,
		"data": map[string]any{"result": []any{
			map[string]any{"mid": 10001, "uname": "测试 UP", "fans": 128000, "upic": "//i0.hdslb.com/test.jpg"},
		}},
	})}
	users, err := searchBilibiliWithActions(context.Background(), fake, "测试 UP")
	if err != nil {
		t.Fatalf("searchBilibiliWithActions() error = %v", err)
	}
	if len(users) != 1 || users[0].UID != "10001" || users[0].Name != "测试 UP" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if len(fake.httpRequests) != 1 {
		t.Fatalf("HTTP requests = %d, want 1 cached signed search request", len(fake.httpRequests))
	}
	parsed, _ := url.Parse(fake.httpRequests[0].URL)
	if parsed.Path != "/x/web-interface/wbi/search/type" || parsed.Query().Get("w_rid") == "" {
		t.Fatalf("search URL did not use signed WBI endpoint: %s", fake.httpRequests[0].URL)
	}
	if !strings.Contains(fake.httpRequests[0].Headers["Cookie"], "SESSDATA=primary") {
		t.Fatalf("search request did not use configured account")
	}
}

func TestBilibiliSearchRotatesAccountsAfterRiskControl(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary", "backup")
	seedSourceState(fake, time.Now(), "", "primary", "backup")
	fake.httpResponses = []rayleabot.ActionResult{
		httpJSONResult(412, map[string]any{"code": -412, "message": "request was banned"}),
		httpJSONResult(200, map[string]any{
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
	if len(fake.httpRequests) != 2 || !strings.Contains(fake.httpRequests[0].Headers["Cookie"], "primary") || !strings.Contains(fake.httpRequests[1].Headers["Cookie"], "backup") {
		t.Fatalf("account rotation requests = %#v", fake.httpRequests)
	}
}

func TestBilibiliUserLookupUsesWBIEndpoint(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpResponses = []rayleabot.ActionResult{httpJSONResult(200, map[string]any{
		"code": 0,
		"data": map[string]any{"mid": 123456, "name": "测试 UP", "face": "//i0.hdslb.com/face.jpg"},
	})}
	user, err := readBilibiliUserWithActions(context.Background(), fake, "123456")
	if err != nil || user.UID != "123456" || user.Name != "测试 UP" || user.AvatarURL != "https://i0.hdslb.com/face.jpg" {
		t.Fatalf("WBI user lookup user=%#v, err=%v", user, err)
	}
	if len(fake.httpRequests) != 1 {
		t.Fatalf("user lookup requests = %#v", fake.httpRequests)
	}
	parsed, _ := url.Parse(fake.httpRequests[0].URL)
	if parsed.Path != "/x/space/wbi/acc/info" || parsed.Query().Get("mid") != "123456" || parsed.Query().Get("w_rid") == "" {
		t.Fatalf("user lookup did not use signed WBI endpoint: %s", fake.httpRequests[0].URL)
	}
}

func TestBilibiliSourceRotatesAccountsAfterRiskControl(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary", "backup")
	now := time.Unix(1780905600, 0)
	seedSourceState(fake, now, "123456", "primary", "backup")
	fake.httpResponses = []rayleabot.ActionResult{
		httpJSONResult(412, map[string]any{"code": -412, "message": "request was banned"}),
		httpJSONResult(200, map[string]any{"code": 0, "data": map[string]any{"items": []any{}}}),
	}
	source := newBilibiliSource(fake)
	source.client.now = func() time.Time { return now }
	result := source.poll(context.Background(), []subscription{{
		ID: "bilibili-one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true,
	}})
	if !result.DynamicOK || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "风控") {
		t.Fatalf("unexpected source result: %#v", result)
	}
	if len(fake.httpRequests) != 2 || !strings.Contains(fake.httpRequests[0].Headers["Cookie"], "primary") || !strings.Contains(fake.httpRequests[1].Headers["Cookie"], "backup") {
		t.Fatalf("account rotation requests = %#v", fake.httpRequests)
	}
	if _, exists := fake.kv["source:bilibili:cooldown:dynamic:primary"]; !exists {
		t.Fatalf("risk-controlled account did not enter cooldown: %#v", fake.kv)
	}
	if len(fake.logs) == 0 || fake.logs[0].Message != "Bilibili 订阅源检查失败" {
		t.Fatalf("risk-control failure was not logged: %#v", fake.logs)
	}
}

func TestBilibiliSourceRefreshesWBIKeysAfterSignatureRejection(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	now := time.Now()
	fake.kv["source:bilibili:follow:primary:123456"] = map[string]any{"checked_at": now.Unix(), "following": true}
	fake.httpResponses = []rayleabot.ActionResult{
		httpJSONResult(200, fixtureNavDocument()),
		httpJSONResult(200, map[string]any{"code": -403, "message": "invalid w_rid"}),
		httpJSONResult(200, fixtureNavDocument()),
		dynamicFeedResult(),
	}
	source := newBilibiliSource(fake)
	result := source.poll(context.Background(), []subscription{{
		ID: "bilibili-one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true,
	}})
	if !result.DynamicOK || len(result.Errors) != 0 {
		t.Fatalf("signature refresh result = %#v", result)
	}
	if len(fake.httpRequests) != 4 || fake.httpRequests[0].URL != bilibiliNavURL || fake.httpRequests[2].URL != bilibiliNavURL {
		t.Fatalf("signature refresh requests = %#v", fake.httpRequests)
	}
}

func TestAccountFeedCollectsMultipleSubjectsInOneRequest(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	now := time.Now()
	seedSourceState(fake, now, "123456", "primary")
	fake.kv["source:bilibili:follow:primary:654321"] = map[string]any{"checked_at": now.Unix(), "following": true}
	second := videoDynamic("second", "第二位 UP 的视频", 1700000060)
	second["modules"].(map[string]any)["module_author"].(map[string]any)["mid"] = "654321"
	fake.httpResponses = []rayleabot.ActionResult{dynamicFeedResult(
		videoDynamic("first", "第一位 UP 的视频", 1700000000), second,
	)}
	result := newBilibiliSource(fake).poll(context.Background(), []subscription{
		{ID: "one", Platform: "bilibili", UID: "123456", Services: []string{"video"}, Enabled: true},
		{ID: "two", Platform: "bilibili", UID: "654321", Services: []string{"video"}, Enabled: true},
	})
	if !result.DynamicOK || len(result.Updates) != 2 {
		t.Fatalf("multi-subject result = %#v", result)
	}
	if len(fake.httpRequests) != 1 || !strings.Contains(fake.httpRequests[0].URL, "/feed/all") {
		t.Fatalf("account feed requests = %#v", fake.httpRequests)
	}
}

func TestSubscriptionCheckBaselinesThenRendersNewDynamic(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	now := time.Now()
	seedSourceState(fake, now, "123456", "primary")
	current := settings{Enabled: true, Subscriptions: []subscription{{
		ID: "bilibili-123456-group-10000", Platform: "bilibili", UID: "123456", Name: "测试 UP",
		TargetType: "group", TargetID: "10000", Services: []string{"video"}, Enabled: true,
		Subscribers: []subscriber{{ID: "42", Nickname: "订阅人"}},
	}}}
	fake.httpResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic("old", "已有视频", 1700000000))}
	first := checkSubscriptionsWithActions(context.Background(), fake, current)
	if intScalar(first["sent"]) != 0 || !boolScalar(fake.kv[dynamicSourceKey(current.Subscriptions[0])]) {
		t.Fatalf("first check did not establish baseline: result=%#v kv=%#v", first, fake.kv)
	}
	if len(fake.renders) != 0 || len(fake.messages) != 0 {
		t.Fatalf("baseline emitted historical content")
	}
	fake.httpResponses = []rayleabot.ActionResult{dynamicFeedResult(
		videoDynamic("new", "新视频", 1700000060),
		videoDynamic("old", "已有视频", 1700000000),
	)}
	second := checkSubscriptionsWithActions(context.Background(), fake, current)
	if intScalar(second["sent"]) != 1 || boolScalar(second["degraded"]) {
		t.Fatalf("second check result = %#v", second)
	}
	if len(fake.renders) != 1 || len(fake.messages) != 1 {
		t.Fatalf("render/message counts = %d/%d", len(fake.renders), len(fake.messages))
	}
	if fake.renders[0].Data["title"] != "新视频" || fake.renders[0].Data["service"] != "视频" {
		t.Fatalf("rich render data was not restored: %#v", fake.renders[0].Data)
	}
	if _, exists := fake.kv["seen:bilibili-123456-group-10000:video:new"]; !exists {
		t.Fatalf("new update was not marked seen")
	}
}

func TestSubscriptionCheckContinuesFanoutAfterTargetFailure(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	now := time.Now()
	seedSourceState(fake, now, "123456", "primary")
	subscriptions := []subscription{
		{ID: "target-one", Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "10000", Services: []string{"video"}, Enabled: true},
		{ID: "target-two", Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "20000", Services: []string{"video"}, Enabled: true},
	}
	for _, item := range subscriptions {
		fake.kv[dynamicSourceKey(item)] = true
	}
	fake.httpResponses = []rayleabot.ActionResult{dynamicFeedResult(videoDynamic("new", "新视频", 1700000060))}
	fake.messageErrors = []error{errors.New("target unavailable"), nil}
	result := checkSubscriptionsWithActions(context.Background(), fake, settings{Enabled: true, Subscriptions: subscriptions})
	if intScalar(result["sent"]) != 1 || !boolScalar(result["degraded"]) || len(fake.messages) != 2 {
		t.Fatalf("fanout result=%#v messages=%#v", result, fake.messages)
	}
	if _, exists := fake.kv["seen:target-one:video:new"]; exists {
		t.Fatalf("failed target was incorrectly marked seen")
	}
	if _, exists := fake.kv["seen:target-two:video:new"]; !exists {
		t.Fatalf("successful target was not marked seen")
	}
}

func TestLiveSourceBaselinesThenEmitsTransition(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	liveDocument := func(status int) rayleabot.ActionResult {
		return httpJSONResult(200, map[string]any{"code": 0, "data": map[string]any{
			"123456": map[string]any{"uid": 123456, "uname": "测试 UP", "room_id": 777, "title": "直播标题", "live_status": status, "live_time": 1700000000},
		}})
	}
	source := newBilibiliSource(fake)
	fake.httpResponses = []rayleabot.ActionResult{liveDocument(1)}
	first := source.poll(context.Background(), []subscription{{
		ID: "live-one", Platform: "bilibili", UID: "123456", Services: []string{"live"}, Enabled: true,
	}})
	if !first.LiveOK || len(first.Updates) != 0 {
		t.Fatalf("live baseline result = %#v", first)
	}
	fake.httpResponses = []rayleabot.ActionResult{liveDocument(0)}
	second := source.poll(context.Background(), []subscription{{
		ID: "live-one", Platform: "bilibili", UID: "123456", Services: []string{"live"}, Enabled: true,
	}})
	if len(second.Updates) != 1 || stringScalar(second.Updates[0]["live_event"]) != "ended" {
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
	if stringScalar(nestedValue(update, "topic", "name")) != "测试活动 2026" || !strings.Contains(stringScalar(update["summary_html"]), "rich-text-emoji") {
		t.Fatalf("rich content was lost: %#v", update)
	}
	if images := imageMaps(update["images"], 9); len(images) != 1 || stringScalar(images[0]["url"]) != "https://i0.hdslb.com/pic.jpg" {
		t.Fatalf("image normalization was lost: %#v", update["images"])
	}
}

func TestBilibiliDiagnosticsRedactCredentials(t *testing.T) {
	response := rayleabot.ActionResult{
		"status_code": 412,
		"body_text":   `{"code":-412,"message":"blocked SESSDATA=private; bili_jct=csrf; Authorization: Bearer token-value"}`,
	}
	document := decodeBilibiliDocument(response)
	message := bilibiliDiagnosticText(response, document)
	if strings.Contains(message, "private") || strings.Contains(message, "csrf") || strings.Contains(message, "token-value") {
		t.Fatalf("diagnostic leaked credentials: %s", message)
	}
	for _, marker := range []string{"SESSDATA=[已隐藏]", "bili_jct=[已隐藏]", "Authorization: [已隐藏]"} {
		if !strings.Contains(message, marker) {
			t.Fatalf("diagnostic missing redaction %q: %s", marker, message)
		}
	}
	quoted := diagnosticExcerpt(`{"cookie":"SESSDATA=quoted-secret", "authorization":"Bearer quoted-token", "refresh_token":"refresh-secret"}`, 500)
	for _, secret := range []string{"quoted-secret", "quoted-token", "refresh-secret"} {
		if strings.Contains(quoted, secret) {
			t.Fatalf("quoted diagnostic leaked %q: %s", secret, quoted)
		}
	}
}

func TestRenderDataTruncatesRichHTMLWithoutBreakingMarkup(t *testing.T) {
	body := strings.Repeat("长正文", 150)
	data := buildBilibiliRenderData(subscription{UID: "123456", Name: "测试 UP"}, map[string]any{
		"title": "测试动态", "service": "image_text", "summary": body,
		"summary_html": `<span class="rich-text-topic">#话题#</span><span>` + body + `</span>`,
	})
	rendered := stringScalar(data["summary_html"])
	if !strings.Contains(rendered, "...") || !strings.HasSuffix(rendered, "</span>") {
		t.Fatalf("rich HTML was not safely truncated: %s", rendered)
	}
	if visible := bilibiliHTMLVisibleLength(rendered); visible > 423 {
		t.Fatalf("rich HTML visible length = %d, want <= 423", visible)
	}
}

func TestPreviewURLParserCoversHistoricalLinkKinds(t *testing.T) {
	tests := map[string]string{
		"https://www.bilibili.com/video/BV1abc123": "video",
		"https://www.bilibili.com/opus/1000000001": "opus",
		"https://t.bilibili.com/1000000002":        "dynamic",
		"https://live.bilibili.com/777":            "live",
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

func seedSourceState(fake *fakePluginActions, now time.Time, uid string, accountIDs ...string) {
	for _, accountID := range accountIDs {
		fake.kv["source:bilibili:wbi:"+accountID] = map[string]any{
			"img_key": "7cd084941338484aae1ad9425b84077c", "sub_key": "4932caff0ff746eab6f01bf08b70ac45", "expires_at": now.Add(time.Hour).Unix(),
		}
		fake.kv["source:bilibili:follow:"+accountID+":"+uid] = map[string]any{"checked_at": now.Unix(), "following": true}
	}
}

func httpJSONResult(status int, document map[string]any) rayleabot.ActionResult {
	raw, _ := json.Marshal(document)
	return rayleabot.ActionResult{"status_code": status, "body_text": string(raw), "headers": map[string]any{}}
}

func dynamicFeedResult(items ...map[string]any) rayleabot.ActionResult {
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, item)
	}
	return httpJSONResult(200, map[string]any{"code": 0, "data": map[string]any{"items": values}})
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
