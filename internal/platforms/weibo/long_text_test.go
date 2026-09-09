package weibo

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestWeiboLongTextEndpointTrimsID(t *testing.T) {
	if got := weiboLongTextEndpoint(" 5000000000000001 "); got != "https://m.weibo.cn/statuses/extend?id=5000000000000001" {
		t.Fatalf("weiboLongTextEndpoint() = %q", got)
	}
}

func TestWeiboNormalizationUsesEmbeddedLongText(t *testing.T) {
	mblog := weiboTextMblog("long-embedded", "6000000001", `摘要...<a href="/status/long-embedded">全文</a>`, 1700000000)
	mblog["isLongText"] = true
	mblog["longText"] = map[string]any{"longTextContent": "完整第一行<br>完整第二行"}

	update := normalizeWeiboMblog(time.UTC, mblog, 0)
	if got := plugin.StringScalar(update["summary"]); got != "完整第一行\n完整第二行" {
		t.Fatalf("embedded long text = %q", got)
	}
	if plugin.BoolScalar(update["needs_long_text"]) {
		t.Fatalf("embedded long text still requested expansion: %#v", update)
	}
}

func TestWeiboNormalizationDetectsTerminalFullTextLink(t *testing.T) {
	update := normalizeWeiboMblog(time.UTC, weiboTextMblog(
		"long-link", "6000000001", `摘要…<a href="/status/long-link">全文</a>`, 1700000000,
	), 0)
	if !plugin.BoolScalar(update["needs_long_text"]) {
		t.Fatalf("terminal full-text link did not request expansion: %#v", update)
	}
}

func TestWeiboLongTextResolverSkipsShortUpdates(t *testing.T) {
	fake := testkit.NewActions()
	update := normalizeWeiboMblog(time.UTC, weiboTextMblog("short", "6000000001", "短微博", 1700000000), 0)

	prepared, degraded := newWeiboLongTextResolver(fake, nil).Prepare(context.Background(), update)
	if degraded || plugin.StringScalar(prepared["summary"]) != "短微博" || len(fake.HTTPRequests) != 0 {
		t.Fatalf("short update triggered expansion: prepared=%#v degraded=%v requests=%#v", prepared, degraded, fake.HTTPRequests)
	}
}

func TestWeiboSubscriptionCheckExpandsAndCachesLongText(t *testing.T) {
	fake := testkit.NewActions()
	fake.Accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	first := plugin.Subscription{
		ID: "long-one", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "private", TargetID: "10001", Services: []string{"post"}, Enabled: true,
	}
	second := first
	second.ID, second.TargetID = "long-two", "10002"
	fake.KV[weiboFeedSourceKey(first)] = true
	fake.KV[weiboFeedSourceKey(second)] = true
	mblog := weiboTextMblog("long-shared", first.UID, `摘要…<a href="/status/long-shared">全文</a>`, now.Unix())
	mblog["isLongText"] = true
	fullText := strings.Repeat("完整正文", 110) + "<br>第二行"
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", Result: weiboFeedResult(mblog)},
		{Path: "/statuses/extend", Result: testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"longTextContent": fullText}})},
		{Path: "/face.jpg", Result: testkit.AvatarHTTPResult()},
	}

	result := checkAt(t, context.Background(), fake, plugin.Settings{
		Enabled: true, Subscriptions: []plugin.Subscription{first, second},
	}, now)

	if plugin.IntScalar(result["sent"]) != 2 || plugin.BoolScalar(result["degraded"]) {
		t.Fatalf("long-text delivery result = %#v", result)
	}
	if got := countHTTPByPath(fake, "/statuses/extend"); got != 1 {
		t.Fatalf("long-text requests = %d, want 1: %#v", got, testkit.RequestURLs(fake))
	}
	want := weiboPlainText(fullText)
	if len(fake.Renders) != 2 {
		t.Fatalf("renders = %d, want 2", len(fake.Renders))
	}
	for _, render := range fake.Renders {
		if got := plugin.StringScalar(render.Data["content_text"]); got != want || strings.Contains(got, "全文") {
			t.Fatalf("expanded render text = %q, want %q", got, want)
		}
	}
}

func TestWeiboSubscriptionCheckExpandsRepostedLongText(t *testing.T) {
	fake := testkit.NewActions()
	fake.Accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := plugin.Subscription{
		ID: "long-repost", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "private", TargetID: "10001", Services: []string{"repost"}, Enabled: true,
	}
	fake.KV[weiboFeedSourceKey(item)] = true
	mblog := weiboTextMblog("repost-long", item.UID, "转发评论", now.Unix())
	mblog["retweeted_status"] = map[string]any{
		"mid": "5000000000000008", "idstr": "5000000000000008", "isLongText": true,
		"text": `原微博摘要...<a href="/status/5000000000000008">全文</a>`, "created_timestamp": now.Add(-time.Minute).Unix(),
		"user": map[string]any{"id": "6000000008", "screen_name": "原作者"},
	}
	fullText := strings.Repeat("原微博完整正文", 45)
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", Result: weiboFeedResult(mblog)},
		{Path: "/statuses/extend", Result: testkit.HTTPJSON(200, map[string]any{"ok": 1, "data": map[string]any{"longTextContent": fullText}})},
		{Path: "/face.jpg", Result: testkit.AvatarHTTPResult()},
	}

	result := checkAt(t, context.Background(), fake, plugin.Settings{
		Enabled: true, Subscriptions: []plugin.Subscription{item},
	}, now)

	if plugin.IntScalar(result["sent"]) != 1 || plugin.BoolScalar(result["degraded"]) || len(fake.Renders) != 1 {
		t.Fatalf("repost long-text result = %#v renders=%d", result, len(fake.Renders))
	}
	original := plugin.MapValue(fake.Renders[0].Data["original"])
	if got := plugin.StringScalar(original["summary"]); got != fullText || strings.Contains(got, "全文") {
		t.Fatalf("expanded original = %q, want %q", got, fullText)
	}
	if got := plugin.StringScalar(fake.Renders[0].Data["content_text"]); got != "转发评论" {
		t.Fatalf("main repost text changed during original expansion: %q", got)
	}
	if got := countHTTPByPath(fake, "/statuses/extend"); got != 1 {
		t.Fatalf("reposted long-text requests = %d, want 1: %#v", got, testkit.RequestURLs(fake))
	}
}

func TestWeiboLongTextResolverCachesFailures(t *testing.T) {
	fake := testkit.NewActions()
	fake.Accounts = fixtureWeiboAccounts("primary")
	fake.HTTPRoutes = []testkit.HTTPRoute{{
		Path:   "/statuses/extend",
		Result: testkit.HTTPJSON(403, map[string]any{"ok": 0, "msg": "登录"}),
	}}
	accounts, err := readWeiboAccounts(context.Background(), fake)
	if err != nil {
		t.Fatalf("readWeiboAccounts() error = %v", err)
	}
	resolver := newWeiboLongTextResolver(fake, accounts)
	update := map[string]any{"id": "long-cached-failure", "summary": "缓存失败摘要…全文", "needs_long_text": true}

	for attempt := 0; attempt < 2; attempt++ {
		prepared, degraded := resolver.Prepare(context.Background(), update)
		if !degraded || plugin.StringScalar(prepared["summary"]) != "缓存失败摘要\n\n正文获取不完整，请查看原微博链接。" {
			t.Fatalf("cached failure attempt %d = %#v degraded=%v", attempt+1, prepared, degraded)
		}
	}
	if got := countHTTPByPath(fake, "/statuses/extend"); got != 1 {
		t.Fatalf("cached failure requests = %d, want 1: %#v", got, testkit.RequestURLs(fake))
	}
	if len(fake.Logs) != 1 {
		t.Fatalf("cached failure logs = %d, want 1: %#v", len(fake.Logs), fake.Logs)
	}
}

func TestWeiboSubscriptionCheckFallsBackToSummaryAndLink(t *testing.T) {
	fake := testkit.NewActions()
	fake.Accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := plugin.Subscription{
		ID: "long-fallback", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "private", TargetID: "10001", Services: []string{"post"}, Enabled: true,
	}
	fake.KV[weiboFeedSourceKey(item)] = true
	mblog := weiboTextMblog("long-failed", item.UID, `摘要...<a href="/status/long-failed">全文</a>`, now.Unix())
	mblog["isLongText"] = true
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/api/container/getIndex", Result: weiboFeedResult(mblog)},
		{Path: "/statuses/extend", Result: testkit.HTTPJSON(403, map[string]any{"ok": 0, "msg": "登录"})},
		{Path: "/face.jpg", Result: testkit.AvatarHTTPResult()},
	}

	result := checkAt(t, context.Background(), fake, plugin.Settings{
		Enabled: true, Subscriptions: []plugin.Subscription{item},
	}, now)

	if plugin.IntScalar(result["sent"]) != 1 || !plugin.BoolScalar(result["degraded"]) || len(fake.Renders) != 1 {
		t.Fatalf("fallback result = %#v renders=%d", result, len(fake.Renders))
	}
	data := fake.Renders[0].Data
	want := "摘要\n\n正文获取不完整，请查看原微博链接。"
	if got := plugin.StringScalar(data["content_text"]); got != want || strings.Contains(got, "全文") {
		t.Fatalf("fallback text = %q, want %q", got, want)
	}
	if got := plugin.StringScalar(data["url"]); got == "" {
		t.Fatal("fallback card lost the original Weibo link")
	}
	if _, exists := fake.KV["seen:long-fallback:post:long-failed"]; !exists {
		t.Fatalf("fallback delivery was not marked seen: %#v", fake.KV)
	}
	foundLog := false
	for _, entry := range fake.Logs {
		if entry.Level == "warn" && plugin.StringScalar(entry.Fields["kind"]) == "auth" && plugin.StringScalar(entry.Fields["update_id"]) == "long-failed" {
			foundLog = true
			break
		}
	}
	if !foundLog {
		t.Fatalf("long-text failure was not logged: %#v", fake.Logs)
	}
}

func TestWeiboRenderDataKeepsCompleteSummaries(t *testing.T) {
	mainText := strings.Repeat("主微博正文", 100)
	originalText := strings.Repeat("原微博正文", 70)
	data := buildWeiboRenderData(plugin.Subscription{Platform: "weibo", UID: "6000000001"}, map[string]any{
		"id": "complete", "service": "repost", "summary": mainText,
		"author": map[string]any{"uid": "6000000001"},
		"original": map[string]any{
			"id": "complete-original", "service": "post", "summary": originalText,
			"author": map[string]any{"uid": "6000000008"},
		},
	})
	if got := plugin.StringScalar(data["content_text"]); got != mainText {
		t.Fatalf("main summary was truncated: got %d runes want %d", len([]rune(got)), len([]rune(mainText)))
	}
	original := plugin.MapValue(data["original"])
	if got := plugin.StringScalar(original["summary"]); got != originalText {
		t.Fatalf("original summary was truncated: got %d runes want %d", len([]rune(got)), len([]rune(originalText)))
	}
}
