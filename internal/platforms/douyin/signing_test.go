package douyin

import (
	"context"
	"net/url"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestDouyinSearchRequestsCarrySessionTokens(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"},
	))}

	if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
		t.Fatalf("searchDouyinWithActions() error = %v", err)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("signed search requests = %#v", testkit.RequestURLs(fake))
	}
	request := fake.HTTPRequests[1]
	parsed, err := url.Parse(request.URL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	// 实验性移除 a_bogus：Go 移植的签名可能被平台识别为异常（verify_check）。
	if parsed.Query().Get("a_bogus") != "" {
		t.Fatalf("search URL should not carry a_bogus: %s", request.URL)
	}
	if request.Headers["User-Agent"] != douyinUserAgent {
		t.Fatalf("signed request UA = %q", request.Headers["User-Agent"])
	}
	// msToken 来自 CK（bootstrap 未下发时回退），verifyFp/fp 与浏览器一致追加。
	if parsed.Query().Get("msToken") != "fixture-mstoken-primary" {
		t.Fatalf("search URL lost cookie msToken: %s", request.URL)
	}
	if parsed.Query().Get("verifyFp") != "verify_fixture_primary" || parsed.Query().Get("fp") != "verify_fixture_primary" {
		t.Fatalf("search URL lost verifyFp/fp: %s", request.URL)
	}
}

func TestDouyinSessionCookiesBootstrapWhenTTWIDMissing(t *testing.T) {
	fake := newActions()
	fake.Accounts = rayleabot.ActionResult{"accounts": []any{map[string]any{
		"account_id": "primary",
		"cookie":     map[string]any{"value": "sessionid=primary;"},
	}}}
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{
			Path: "/",
			Result: rayleabot.ActionResult{
				"status_code": 200,
				"headers":     map[string]any{"Set-Cookie": "msToken=bootstrapped-token; Path=/; Domain=.douyin.com"},
				"body_text":   "<html></html>",
			},
		},
		{
			Path: "/ttwid/union/register/",
			Result: rayleabot.ActionResult{
				"status_code": 200,
				"headers":     map[string]any{"Set-Cookie": "ttwid=bootstrapped-ttwid; Path=/; Domain=.bytedance.com"},
				"body_text":   `{"status_code":0}`,
			},
		},
		{Path: "/webid", Result: testkit.HTTPJSON(200, map[string]any{"web_id": "7000000000000000002"})},
		{Path: "/aweme/v1/web/discover/search/", Result: testkit.HTTPJSON(200, douyinSearchDocument(
			map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"},
		))},
	}

	if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
		t.Fatalf("searchDouyinWithActions() error = %v", err)
	}
	if len(fake.HTTPRequests) != 5 {
		t.Fatalf("requests = %#v", testkit.RequestURLs(fake))
	}
	for i, host := range []string{"www.douyin.com", "ttwid.bytedance.com", "mcs.zijieapi.com"} {
		if parsed, _ := url.Parse(fake.HTTPRequests[i].URL); parsed == nil || parsed.Host != host {
			t.Fatalf("bootstrap request %d should hit %s: %s", i, host, fake.HTTPRequests[i].URL)
		}
	}
	searchRequest := fake.HTTPRequests[4]
	cookie := searchRequest.Headers["Cookie"]
	if !strings.Contains(cookie, "sessionid=primary") || !strings.Contains(cookie, "ttwid=bootstrapped-ttwid") {
		t.Fatalf("search request lost session cookies: %q", cookie)
	}
	parsed, _ := url.Parse(searchRequest.URL)
	if parsed.Query().Get("msToken") != "bootstrapped-token" || parsed.Query().Get("webid") != "7000000000000000002" {
		t.Fatalf("search request lost msToken/webid: %s", searchRequest.URL)
	}
	if parsed.Query().Get("verifyFp") == "" || parsed.Query().Get("fp") == "" {
		t.Fatalf("search request lost verifyFp/fp: %s", searchRequest.URL)
	}
	if _, exists := fake.KV["source:douyin:web_cookies:primary"]; !exists {
		t.Fatalf("bootstrapped cookies were not cached: %#v", fake.KV)
	}
}

func TestDouyinSessionCookiesSkipBootstrapWhenTTWIDPresent(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"},
	))}

	if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
		t.Fatalf("searchDouyinWithActions() error = %v", err)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("unexpected bootstrap request: %#v", testkit.RequestURLs(fake))
	}
	cookie := fake.HTTPRequests[1].Headers["Cookie"]
	if !strings.Contains(cookie, "ttwid=fixture-ttwid-primary") {
		t.Fatalf("request lost existing ttwid: %q", cookie)
	}
}

func TestDouyinSessionCookiesNegativeCacheAvoidsRepeatBootstrap(t *testing.T) {
	fake := newActions()
	fake.Accounts = rayleabot.ActionResult{"accounts": []any{map[string]any{
		"account_id": "primary",
		"cookie":     map[string]any{"value": "sessionid=primary;"},
	}}}
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/aweme/v1/web/discover/search/", Result: testkit.HTTPJSON(200, douyinSearchDocument(
			map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"},
		))},
	}
	for round := 0; round < 2; round++ {
		if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
			t.Fatalf("round %d search error = %v", round, err)
		}
	}
	bootstraps := 0
	for _, request := range fake.HTTPRequests {
		if parsed, _ := url.Parse(request.URL); parsed != nil && parsed.Host == "ttwid.bytedance.com" {
			bootstraps++
		}
	}
	if bootstraps != 1 {
		t.Fatalf("bootstrap requests = %d, want 1: %#v", bootstraps, testkit.RequestURLs(fake))
	}
}

func TestDouyinSignAPIURLBindsCookieTokens(t *testing.T) {
	client := newDouyinClient(newActions())
	signed := client.signAPIURL(douyinSearchAPIURLs("测试")[0], douyinSession{
		Cookie: "sessionid=x; msToken=cookie-token; s_v_web_id=verify-fixture;", WebID: "7000000000000000001",
	})
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	query := parsed.Query()
	if query.Get("msToken") != "cookie-token" || query.Get("webid") != "7000000000000000001" {
		t.Fatalf("signed query = %s", parsed.RawQuery)
	}
	// 实验性移除 a_bogus 后签名参数不再出现。
	if query.Get("a_bogus") != "" {
		t.Fatalf("signed query should not carry a_bogus: %s", parsed.RawQuery)
	}
	if query.Get("verifyFp") != "verify-fixture" || query.Get("fp") != "verify-fixture" {
		t.Fatalf("signed query lost verifyFp/fp: %s", parsed.RawQuery)
	}
	// 与真实浏览器一致：verifyFp/fp 追加在会话参数之后。
	if !strings.Contains(parsed.RawQuery, "&verifyFp=verify-fixture&fp=verify-fixture") {
		t.Fatalf("verifyFp/fp should follow session params: %s", parsed.RawQuery)
	}
	// CK 没有 msToken 时不再生成随机令牌，保持 URL 原样（设备不绑定）。
	fallback := client.signAPIURL(douyinSearchAPIURLs("测试")[0], douyinSession{Cookie: "sessionid=x;"})
	if parsed, _ := url.Parse(fallback); parsed.Query().Get("msToken") != "" {
		t.Fatalf("fallback should not invent msToken: %s", fallback)
	}
}

func TestDouyinSessionPersistsFingerprintAndVerifyFp(t *testing.T) {
	fake := newActions()
	account := douyinAccount{ID: "primary", Cookie: "sessionid=primary; ttwid=fixture-ttwid; msToken=fixture-mstoken;"}

	first := newDouyinClient(fake).session(context.Background(), account)
	// 模拟插件重启：新客户端应从 KV 读回同一指纹与 s_v_web_id。
	second := newDouyinClient(fake).session(context.Background(), account)
	if first.FP == "" || first.FP != second.FP {
		t.Fatalf("fingerprint was not persisted across clients: %q vs %q", first.FP, second.FP)
	}
	firstID := plugin.CookieField(first.Cookie, "s_v_web_id")
	secondID := plugin.CookieField(second.Cookie, "s_v_web_id")
	if firstID == "" || firstID != secondID || !strings.HasPrefix(firstID, "verify_") {
		t.Fatalf("s_v_web_id was not persisted across clients: %q vs %q", firstID, secondID)
	}
	if !strings.Contains(first.Cookie, "ttwid=fixture-ttwid") || !strings.Contains(first.Cookie, "msToken=fixture-mstoken") {
		t.Fatalf("session lost original cookies: %q", first.Cookie)
	}
}

func TestDouyinErrorLogFieldsCarryStatusMsgAndEndpoint(t *testing.T) {
	err := &douyinSourceError{
		Kind: "risk_control", HTTPStatus: 200, StatusMsg: "为了你的账号安全，请先完成验证",
		Endpoint: "www.douyin.com/aweme/v1/web/search/item",
	}
	fields := douyinErrorLogFields(err)
	if fields["kind"] != "risk_control" || fields["http_status"] != 200 {
		t.Fatalf("fields = %#v", fields)
	}
	if fields["status_msg"] != "为了你的账号安全，请先完成验证" || fields["endpoint"] != "www.douyin.com/aweme/v1/web/search/item" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestDouyinSessionPrefersBootstrappedMsToken(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	freshToken := strings.Repeat("a", 182) + "=="
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/web/r/token", Result: rayleabot.ActionResult{
			"status_code": 200,
			"headers":     map[string]any{"Set-Cookie": "msToken=" + freshToken + "; Path=/; Domain=.bytedance.com"},
		}},
	}
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"},
	))}

	if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
		t.Fatalf("searchDouyinWithActions() error = %v", err)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("requests = %#v", testkit.RequestURLs(fake))
	}
	searchRequest := fake.HTTPRequests[1]
	parsed, _ := url.Parse(searchRequest.URL)
	if parsed.Query().Get("msToken") != freshToken {
		t.Fatalf("search used stale msToken instead of the bootstrapped one: %s", searchRequest.URL)
	}
	// 引导得到的真实 msToken 必须覆盖 CK 里的旧值（fixture-mstoken-primary）。
	if !strings.Contains(searchRequest.Headers["Cookie"], "msToken="+freshToken) || strings.Contains(searchRequest.Headers["Cookie"], "fixture-mstoken-primary") {
		t.Fatalf("search cookie did not carry fresh msToken: %q", searchRequest.Headers["Cookie"])
	}
}

func TestDouyinMsTokenBootstrapNegativeCacheAvoidsRepeatRequests(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, douyinSearchDocument(map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"})),
		testkit.HTTPJSON(200, douyinSearchDocument(map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户"})),
	}
	for round := 0; round < 2; round++ {
		if _, err := searchDouyinWithActions(context.Background(), fake, "测试用户"); err != nil {
			t.Fatalf("round %d search error = %v", round, err)
		}
	}
	mssdkRequests := 0
	for _, request := range fake.HTTPRequests {
		if parsed, _ := url.Parse(request.URL); parsed != nil && parsed.Host == "mssdk.bytedance.com" {
			mssdkRequests++
		}
	}
	if mssdkRequests != 1 {
		t.Fatalf("mssdk requests = %d, want 1: %#v", mssdkRequests, testkit.RequestURLs(fake))
	}
	// 引导失败（fake 未下发 msToken）时回退 CK 里的 msToken。
	searchRequest := fake.HTTPRequests[1]
	if parsed, _ := url.Parse(searchRequest.URL); parsed.Query().Get("msToken") != "fixture-mstoken-primary" {
		t.Fatalf("search did not fall back to cookie msToken: %s", searchRequest.URL)
	}
}
