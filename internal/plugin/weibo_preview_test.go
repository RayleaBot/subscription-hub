package plugin

import (
	"context"
	"strings"
	"testing"
)

func TestParsePreviewInputSelectsWeiboAndBilibili(t *testing.T) {
	tests := []struct {
		input        string
		wantPlatform string
		wantService  string
	}{
		{input: "", wantPlatform: "bilibili", wantService: "video"},
		{input: "视频", wantPlatform: "bilibili", wantService: "video"},
		{input: "转发", wantPlatform: "bilibili", wantService: "repost"},
		{input: "直播", wantPlatform: "bilibili", wantService: "live"},
		{input: "图文", wantPlatform: "bilibili", wantService: "image_text"},
		{input: "动态", wantPlatform: "bilibili", wantService: "image_text"},
		{input: "微博", wantPlatform: "weibo", wantService: "post"},
		{input: "图片", wantPlatform: "weibo", wantService: "image"},
		{input: "文字", wantPlatform: "weibo", wantService: "post"},
		{input: "微博 视频", wantPlatform: "weibo", wantService: "video"},
		{input: "微博 转发", wantPlatform: "weibo", wantService: "repost"},
		{input: "微博视频", wantPlatform: "weibo", wantService: "video"},
		{input: "微博图片", wantPlatform: "weibo", wantService: "image"},
		{input: "b站 转发", wantPlatform: "bilibili", wantService: "repost"},
		{input: "b站视频", wantPlatform: "bilibili", wantService: "video"},
	}
	for _, test := range tests {
		platform, service := parsePreviewInput(test.input)
		if platform != test.wantPlatform || service != test.wantService {
			t.Fatalf("parsePreviewInput(%q) = %s %s, want %s %s", test.input, platform, service, test.wantPlatform, test.wantService)
		}
	}
}

func TestParseWeiboPreviewURL(t *testing.T) {
	tests := map[string]string{
		"https://m.weibo.cn/status/5000000000000001":    "5000000000000001",
		"https://m.weibo.cn/detail/5000000000000001":    "5000000000000001",
		"https://m.weibo.cn/statuses/show?id=P8abcXYZ":  "P8abcXYZ",
		"https://weibo.com/6000000001/P8abcXYZ":         "P8abcXYZ",
		"https://weibo.com/7198559139/5331543666721397": "5331543666721397",
		"https://www.weibo.com/u/6000000001/5000000001": "5000000001",
		"m.weibo.cn/status/5000000000000001":            "5000000000000001",
	}
	for rawURL, id := range tests {
		ref := parseWeiboPreviewURL(rawURL)
		if ref == nil || ref.ID != id {
			t.Fatalf("parseWeiboPreviewURL(%q) = %#v, want %s", rawURL, ref, id)
		}
	}
	if ref := parseWeiboPreviewURL("https://weibo.com/6000000001/follow"); ref != nil {
		t.Fatalf("profile follow path should not parse as status: %#v", ref)
	}
	if ref := parseWeiboPreviewURL("https://weibo.com/ttarticle/p/show?id=230940"); ref != nil {
		t.Fatalf("article path should not parse as status: %#v", ref)
	}
	if !looksLikeWeiboPreviewURL("https://weibo.com/ttarticle/p/show?id=230940") {
		t.Fatal("article URL should still look like a weibo link")
	}
}

func TestFetchWeiboPreviewUsesMobileStatusAPI(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{{
		path: "/statuses/show",
		result: httpJSONResult(200, map[string]any{"ok": 1, "data": weiboImageMblog(
			"5000000000000001", "6000000001", "预览图片微博", 1700000000,
		)}),
	}}
	update, err := fetchWeiboPreview(context.Background(), fake, parseWeiboPreviewURL("https://m.weibo.cn/status/5000000000000001"))
	if err != nil {
		t.Fatalf("fetchWeiboPreview() error = %v", err)
	}
	if stringScalar(update["id"]) != "5000000000000001" || stringScalar(update["service"]) != "image" || stringScalar(update["uid"]) != "6000000001" {
		t.Fatalf("preview update = %#v", update)
	}
	if len(fake.httpRequests) != 1 || !strings.Contains(fake.httpRequests[0].URL, "/statuses/show") || !strings.Contains(fake.httpRequests[0].URL, "id=5000000000000001") {
		t.Fatalf("preview request = %#v", fake.httpRequests)
	}
	if fake.httpRequests[0].Headers["Referer"] != "https://m.weibo.cn/status/5000000000000001" {
		t.Fatalf("preview referer = %#v", fake.httpRequests[0].Headers)
	}
}

func TestFetchWeiboPreviewExpandsLongText(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	mblog := weiboTextMblog(
		"5000000000000001",
		"6000000001",
		`摘要…<a href="/status/5000000000000001">全文</a>`,
		1700000000,
	)
	mblog["isLongText"] = true
	fullText := strings.Repeat("预览完整正文", 80) + "<br>第二行"
	fake.httpRoutes = []fakeHTTPRoute{
		{
			path:   "/statuses/show",
			result: httpJSONResult(200, map[string]any{"ok": 1, "data": mblog}),
		},
		{
			path:   "/statuses/extend",
			result: httpJSONResult(200, map[string]any{"ok": 1, "data": map[string]any{"longTextContent": fullText}}),
		},
	}

	update, err := fetchWeiboPreview(context.Background(), fake, parseWeiboPreviewURL("https://m.weibo.cn/status/5000000000000001"))
	if err != nil {
		t.Fatalf("fetchWeiboPreview() error = %v", err)
	}
	if got, want := stringScalar(update["summary"]), weiboPlainText(fullText); got != want {
		t.Fatalf("preview long text = %q, want %q", got, want)
	}
	if got := countHTTPByPath(fake, "/statuses/extend"); got != 1 {
		t.Fatalf("preview long-text requests = %d, want 1: %#v", got, requestURLs(fake))
	}
}

func TestSampleWeiboUpdateCoversCatalogServices(t *testing.T) {
	for _, service := range []string{"post", "image", "video", "repost"} {
		update := sampleWeiboUpdate(service)
		if stringScalar(update["platform"]) != "weibo" {
			t.Fatalf("sample %s missing platform: %#v", service, update)
		}
		if service == "post" && stringScalar(update["service"]) != "post" {
			t.Fatalf("post sample service = %#v", update)
		}
		if service == "repost" && mapValue(update["original"]) == nil {
			t.Fatalf("repost sample missing original: %#v", update)
		}
		if service == "image" && len(imageMaps(update["images"], 9)) == 0 {
			t.Fatalf("image sample missing pictures: %#v", update)
		}
		data := buildWeiboRenderData(subscription{Platform: "weibo", UID: "6000000001", Name: "示例博主"}, update)
		if data["platform"] != "微博" {
			t.Fatalf("render platform = %#v", data)
		}
		if images, ok := data["images"].([]map[string]any); !ok || images == nil {
			t.Fatalf("render images must stay a JSON array for %s: %#v", service, data["images"])
		}
		if service == "post" && stringScalar(data["title"]) != "" {
			t.Fatalf("text-only Weibo must not synthesize a title: %#v", data)
		}
	}
}

func TestFetchWeiboPreviewWithoutAccountTriesAnonymousThenGuides(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts()
	fake.httpRoutes = []fakeHTTPRoute{{
		path:   "/statuses/show",
		result: httpJSONResult(403, map[string]any{"ok": 0, "msg": "登录"}),
	}}
	_, err := fetchWeiboPreview(context.Background(), fake, &weiboPreviewRef{ID: "5000000000000001", URL: "https://m.weibo.cn/status/5000000000000001"})
	if err == nil || !strings.Contains(err.Error(), "没有可用的微博账号 CK") {
		t.Fatalf("missing account preview error = %v", err)
	}
}

func TestFetchWeiboPreviewNormalizesShowAPIRubyDateAndCompactAvatar(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{{
		path: "/statuses/show",
		result: httpJSONResult(200, map[string]any{
			"ok": 1,
			"data": map[string]any{
				"created_at": "Thu Aug 13 20:02:08 +0800 2026",
				"id":         "5331543666721397",
				"mid":        "5331543666721397",
				"bid":        "RdeASscxD",
				"text":       "作为一个薄巧爱好者 今天喝了霸王茶姬的薄荷球球 我从来没有喝/吃到过这么像牙膏的薄巧 完全就是。。 牙膏",
				"user": map[string]any{
					"id":                7198559139,
					"screen_name":       "测试博主",
					"profile_image_url": "https://tvax4.sinaimg.cn/crop.0.0.1080.1080.180/face.jpg",
					"avatar_hd":         "https://wx4.sinaimg.cn/orj480/face.jpg",
				},
			},
		}),
	}}
	update, err := fetchWeiboPreview(context.Background(), fake, parseWeiboPreviewURL("https://weibo.com/7198559139/5331543666721397"))
	if err != nil {
		t.Fatalf("fetchWeiboPreview() error = %v", err)
	}
	if stringScalar(update["id"]) != "5331543666721397" || stringScalar(update["uid"]) != "7198559139" {
		t.Fatalf("preview update = %#v", update)
	}
	if intScalar(update["pub_ts"]) <= 0 {
		t.Fatalf("ruby date was not parsed: %#v", update)
	}
	author := mapValue(update["author"])
	avatar := stringScalar(author["avatar"])
	if !strings.Contains(avatar, "/crop.0.0.1080.1080.180/") || strings.Contains(avatar, "/orj480/") {
		t.Fatalf("preview should use compact avatar, got %q", avatar)
	}
	wantText := "作为一个薄巧爱好者 今天喝了霸王茶姬的薄荷球球 我从来没有喝/吃到过这么像牙膏的薄巧 完全就是。。 牙膏"
	if stringScalar(update["title"]) != "" || stringScalar(update["summary"]) != wantText {
		t.Fatalf("normalized preview text = %#v", update)
	}
	data := buildWeiboRenderData(subscription{Platform: "weibo", UID: "7198559139", Name: "测试博主"}, update)
	if stringScalar(data["title"]) != "" || stringScalar(data["headline"]) != "" || stringScalar(data["content_text"]) != wantText || stringScalar(data["platform"]) != "微博" {
		t.Fatalf("render data = %#v", data)
	}
	if got := stringScalar(data["content_html"]); got != "" {
		t.Fatalf("Weibo plain text unexpectedly produced HTML markup: %q", got)
	}
	if !strings.Contains(stringScalar(data["content_text"]), "\u3002\u3002 \u7259\u818f") {
		t.Fatalf("Weibo full-width punctuation was not preserved: %q", data["content_text"])
	}
	if stringScalar(data["service"]) != "文字" || stringScalar(data["source_label"]) != "文字微博" {
		t.Fatalf("localized Weibo labels = %#v", data)
	}
}

func TestFetchWeiboPreviewLogsWhenStatusCannotBeNormalized(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{{
		path: "/statuses/show",
		result: httpJSONResult(200, map[string]any{"ok": 1, "data": map[string]any{
			"id": "5331543666721397", "text": "缺少作者和时间",
		}}),
	}}
	_, err := fetchWeiboPreview(context.Background(), fake, &weiboPreviewRef{ID: "5331543666721397", URL: "https://m.weibo.cn/status/5331543666721397"})
	if err == nil || !strings.Contains(err.Error(), "没有找到这条微博") {
		t.Fatalf("incomplete status error = %v", err)
	}
	if len(fake.logs) == 0 || fake.logs[0].Message != "微博预览解析失败" {
		t.Fatalf("normalize failure was not logged: %#v", fake.logs)
	}
	if stringScalar(fake.logs[0].Fields["reason"]) == "" || stringScalar(fake.logs[0].Fields["weibo_id"]) != "5331543666721397" {
		t.Fatalf("normalize failure log fields = %#v", fake.logs[0].Fields)
	}
}

func TestWeiboCardAvatarURLRewritesHighResolutionHosts(t *testing.T) {
	compact := weiboCardAvatarURL("https://wx4.sinaimg.cn/orj480/face.jpg")
	if compact != "https://wx4.sinaimg.cn/orj180/face.jpg" {
		t.Fatalf("orj avatar compact = %q", compact)
	}
	crop := weiboCardAvatarURL("https://tvax4.sinaimg.cn/crop.0.0.1080.1080.1024/face.jpg?ssig=fixture")
	if crop != "https://tvax4.sinaimg.cn/crop.0.0.1080.1080.180/face.jpg?ssig=fixture" {
		t.Fatalf("crop avatar compact = %q", crop)
	}
}

func TestDiagnosticExcerptRedactsWeiboUserToken(t *testing.T) {
	got := diagnosticExcerpt(`{"user_token":"a0.fixture-token","msg":"ok"}`, 240)
	if strings.Contains(got, "a0.fixture-token") || !strings.Contains(got, "[已隐藏]") {
		t.Fatalf("user_token was not redacted: %q", got)
	}
}
