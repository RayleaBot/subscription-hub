package douyin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func fixtureDouyinAccounts(ids ...string) rayleabot.ActionResult {
	accounts := make([]any, 0, len(ids))
	for _, id := range ids {
		accounts = append(accounts, map[string]any{
			"account_id": id,
			"cookie": map[string]any{"value": "sessionid=" + id + "; ttwid=fixture-ttwid-" + id + "; msToken=fixture-mstoken-" + id +
				"; webid=7000000000000000001; s_v_web_id=verify_fixture_" + id + ";"},
			"profile": map[string]any{"uid": "70000", "nickname": "fixture"},
		})
	}
	return rayleabot.ActionResult{"accounts": accounts}
}

func douyinAwemePostResult(awemes ...map[string]any) rayleabot.ActionResult {
	values := make([]any, 0, len(awemes))
	for _, aweme := range awemes {
		values = append(values, aweme)
	}
	return testkit.HTTPJSON(200, map[string]any{"status_code": 0, "aweme_list": values})
}

func douyinVideoAweme(id, secUID, name, text string, ts int64) map[string]any {
	return map[string]any{
		"aweme_id": id, "aweme_type": 0, "desc": text, "create_time": ts,
		"author": map[string]any{"sec_uid": secUID, "nickname": name},
		"video": map[string]any{
			"duration": 80000,
			"cover":    map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/fixture-cover.jpeg"}},
		},
	}
}

func douyinImageAweme(id, secUID, name, text string, ts int64) map[string]any {
	return map[string]any{
		"aweme_id": id, "aweme_type": 68, "desc": text, "create_time": ts,
		"author": map[string]any{"sec_uid": secUID, "nickname": name},
		"images": []any{
			map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/note-1.jpeg"}},
			map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/note-2.jpeg"}},
		},
	}
}

func douyinSearchDocument(users ...map[string]any) map[string]any {
	list := make([]any, 0, len(users))
	for _, user := range users {
		list = append(list, map[string]any{"user_info": user})
	}
	return map[string]any{"status_code": 0, "user_list": list}
}

func douyinRenderDataPage(document map[string]any) string {
	raw, _ := json.Marshal(document)
	return `<html><head></head><body><script id="RENDER_DATA" type="application/json">` + url.QueryEscape(string(raw)) + `</script></body></html>`
}

func douyinRouterDataPage(document map[string]any) string {
	raw, _ := json.Marshal(document)
	return `<html><head></head><body><script>window._ROUTER_DATA = ` + string(raw) + `</script></body></html>`
}

func TestDouyinFeedNormalizesVideoAndImageText(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	secUID := "MS4wLjABAAAAfixture"
	now := time.Now().Truncate(time.Second)
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(
		douyinVideoAweme("v1", secUID, "测试用户", "新视频", now.Unix()),
		douyinImageAweme("i1", secUID, "测试用户", "新图文", now.Unix()),
	)}

	result := newDouyinSource(fake).poll(context.Background(), []plugin.Subscription{
		{ID: "one", Platform: "douyin", UID: secUID, Services: []string{"all"}, Enabled: true},
	})
	if !result.FeedOK || len(result.Updates) != 2 {
		t.Fatalf("douyin feed result = %#v", result)
	}
	video := result.Updates[0]
	if plugin.StringScalar(video["platform"]) != "douyin" || plugin.StringScalar(video["service"]) != "video" || plugin.StringScalar(video["uid"]) != secUID {
		t.Fatalf("video update lost douyin identity: %#v", video)
	}
	if plugin.StringScalar(video["url"]) != "https://www.douyin.com/video/v1" || plugin.StringScalar(video["duration_text"]) != "1:20" {
		t.Fatalf("video update lost link fields: %#v", video)
	}
	image := result.Updates[1]
	if plugin.StringScalar(image["service"]) != "image_text" || plugin.StringScalar(image["url"]) != "https://www.douyin.com/note/i1" {
		t.Fatalf("image-text update lost douyin fields: %#v", image)
	}
	if len(plugin.ImageMaps(image["images"], 9)) != 2 {
		t.Fatalf("image-text update lost images: %#v", image["images"])
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[1].URL, "www.douyin.com/aweme/v1/web/aweme/post/") {
		t.Fatalf("douyin feed did not use the aweme post endpoint: %#v", testkit.RequestURLs(fake))
	}
}

func TestDouyinSubscriptionCheckBaselinesThenRendersNewAweme(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	now := time.Now().Truncate(time.Second)
	secUID := "MS4wLjABAAAAfixture"
	item := plugin.Subscription{
		ID: "douyin-fixture-group-10000", Platform: "douyin", UID: secUID, Name: "测试用户",
		TargetType: "group", TargetID: "10000", Services: []string{"video"}, Enabled: true,
		Subscribers: []plugin.Subscriber{{ID: "nickname-only", Nickname: "订阅人"}},
	}
	current := plugin.Settings{Enabled: true, Subscriptions: []plugin.Subscription{item}}
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(
		douyinVideoAweme("old", secUID, "测试用户", "已有作品", now.Add(-time.Minute).Unix()),
	)}
	first := checkAt(t, context.Background(), fake, current, now)
	if plugin.IntScalar(first["sent"]) != 0 || !douyinFeedInitialized(context.Background(), fake, item) {
		t.Fatalf("first check did not establish baseline: result=%#v kv=%#v", first, fake.KV)
	}
	if plugin.StringScalar(first["skipped"]) != "" {
		t.Fatalf("douyin-only check was skipped: %#v", first)
	}
	if len(fake.Renders) != 0 || len(fake.Messages) != 0 {
		t.Fatalf("baseline emitted historical content")
	}

	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(
		douyinVideoAweme("new", secUID, "测试用户", "新作品", now.Add(time.Minute).Unix()),
		douyinVideoAweme("old", secUID, "测试用户", "已有作品", now.Add(-time.Minute).Unix()),
	)}
	second := checkAt(t, context.Background(), fake, current, now.Add(2*time.Minute))
	if plugin.IntScalar(second["sent"]) != 1 || plugin.BoolScalar(second["degraded"]) {
		t.Fatalf("second check result = %#v", second)
	}
	if len(fake.Renders) != 1 || fake.Renders[0].Template != "douyin-update" {
		t.Fatalf("unexpected renders = %#v", fake.Renders)
	}
	data := fake.Renders[0].Data
	if plugin.StringScalar(data["content_text"]) != "新作品" || plugin.StringScalar(data["service"]) != "视频" || plugin.StringScalar(data["subtitle"]) != "抖音 · 视频" {
		t.Fatalf("render data lost douyin fields: %#v", data)
	}
	if plugin.StringScalar(data["url"]) != "https://www.douyin.com/video/new" || plugin.StringScalar(data["duration_text"]) != "1:20" {
		t.Fatalf("render data lost media fields: %#v", data)
	}
	if _, exists := fake.KV["seen:douyin-fixture-group-10000:video:new"]; !exists {
		t.Fatalf("new update was not marked seen: %#v", fake.KV)
	}
}

func TestDouyinSubscriptionCheckPushesLiveStartOncePerSession(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	now := time.Now().Truncate(time.Second)
	secUID := "MS4wLjABAAAAfixture"
	item := plugin.Subscription{
		ID: "douyin-live-group-10000", Platform: "douyin", UID: secUID, Name: "测试用户",
		TargetType: "group", TargetID: "10000", Services: []string{"all"}, Enabled: true,
	}
	current := plugin.Settings{Enabled: true, Subscriptions: []plugin.Subscription{item}}
	liveAweme := func(status int) map[string]any {
		aweme := douyinVideoAweme("v1", secUID, "测试用户", "视频", now.Unix())
		author := plugin.MapValue(aweme["author"])
		author["live_status"] = status
		if status == 1 {
			author["room_id"] = "r1"
			author["web_rid"] = "w1"
		}
		return aweme
	}

	// 第一轮：建立作品基线，首次观测直播只记录会话状态。
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(liveAweme(1))}
	first := checkAt(t, context.Background(), fake, current, now)
	if plugin.IntScalar(first["sent"]) != 0 || !douyinFeedInitialized(context.Background(), fake, item) {
		t.Fatalf("first check = %#v", first)
	}
	// 第二轮：明确观测到下播，只记录状态不推送。
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(liveAweme(2))}
	second := checkAt(t, context.Background(), fake, current, now.Add(time.Minute))
	if plugin.IntScalar(second["sent"]) != 0 {
		t.Fatalf("live end emitted a card: %#v", second)
	}
	// 第三轮：再次开播推送开播卡片。
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(liveAweme(1))}
	third := checkAt(t, context.Background(), fake, current, now.Add(2*time.Minute))
	if plugin.IntScalar(third["sent"]) != 1 || plugin.BoolScalar(third["degraded"]) {
		t.Fatalf("live restart was not pushed: %#v", third)
	}
	if len(fake.Renders) != 1 || fake.Renders[0].Template != "douyin-update" {
		t.Fatalf("unexpected live renders = %#v", fake.Renders)
	}
	data := fake.Renders[0].Data
	if plugin.StringScalar(data["service"]) != "直播" || !strings.Contains(plugin.StringScalar(data["summary"]), "直播中") {
		t.Fatalf("live card lost status fields: %#v", data)
	}
	if _, exists := fake.KV["seen:douyin-live-group-10000:live:live-"+secUID+"-started-w1"]; !exists {
		t.Fatalf("live update was not marked seen: %#v", fake.KV)
	}
	// 第四轮：同一会话不重复推送。
	fake.HTTPResponses = []rayleabot.ActionResult{douyinAwemePostResult(liveAweme(1))}
	fourth := checkAt(t, context.Background(), fake, current, now.Add(3*time.Minute))
	if plugin.IntScalar(fourth["sent"]) != 0 {
		t.Fatalf("same live session was pushed again: %#v", fourth)
	}
}

func TestDouyinLiveTransitionKeepsStateWhenLiveUnobserved(t *testing.T) {
	fake := newActions()
	source := newDouyinSource(fake)
	secUID := "MS4wLjABAAAAfixture"
	live := map[string]any{
		"id": "w1", "web_rid": "w1", "room_id": "r1", "session": "1000",
		"title": "测试直播", "url": "https://live.douyin.com/w1",
		"user": map[string]any{"sec_uid": secUID, "nickname": "测试用户"},
	}
	if update := source.liveTransition(context.Background(), secUID, live, true); update != nil {
		t.Fatalf("first observation emitted an update: %#v", update)
	}
	if update := source.liveTransition(context.Background(), secUID, live, true); update != nil {
		t.Fatalf("same session emitted again: %#v", update)
	}
	// 响应没有携带直播状态字段时保持已记录状态，避免误报下播。
	if update := source.liveTransition(context.Background(), secUID, nil, false); update != nil {
		t.Fatalf("unobserved live state emitted a card: %#v", update)
	}
	if update := source.liveTransition(context.Background(), secUID, live, true); update != nil {
		t.Fatalf("kept state re-emitted the same session: %#v", update)
	}
	if update := source.liveTransition(context.Background(), secUID, nil, true); update != nil {
		t.Fatalf("live end emitted a card: %#v", update)
	}
	update := source.liveTransition(context.Background(), secUID, live, true)
	if update == nil || plugin.StringScalar(update["live_event"]) != "started" || plugin.StringScalar(update["id"]) != "live-"+secUID+"-started-1000" {
		t.Fatalf("restarted live did not emit a started update: %#v", update)
	}
}

func TestDouyinSearchParsesUserList(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{
			"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户", "unique_id": "testuser",
			"avatar_larger":  map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/face.jpeg"}},
			"follower_count": 12865000, "signature": "测试简介", "custom_verify": "抖音认证：知名博主",
		},
	))}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err != nil || len(users) != 1 {
		t.Fatalf("searchDouyinWithActions() users=%#v, err=%v", users, err)
	}
	user := users[0]
	if user.UID != "MS4wLjABAAAAone" || user.Name != "测试用户" || user.UniqueID != "testuser" {
		t.Fatalf("search lost identity fields: %#v", user)
	}
	if user.AvatarURL != "https://p3-pc.douyinpic.com/face.jpeg" || user.FansText != "粉丝 1286.5万" || user.Sign != "测试简介" || user.Verify != "抖音认证：知名博主" {
		t.Fatalf("search lost profile fields: %#v", user)
	}
	if len(fake.HTTPRequests) != 2 || !strings.Contains(fake.HTTPRequests[1].URL, "www.douyin.com/aweme/v1/web/discover/search/") {
		t.Fatalf("search did not use the discover/search endpoint: %#v", testkit.RequestURLs(fake))
	}
}

func TestDouyinSearchSkipsHTMLFallbackWhenRiskControlled(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
		"status_code": 0,
		"user_list":   []any{},
		"search_nil_info": map[string]any{
			"search_nil_type": "verify_check",
		},
	})}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err == nil || !strings.Contains(err.Error(), "安全验证") {
		t.Fatalf("searchDouyinWithActions() err=%v, want risk control message", err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %#v, want none", users)
	}
	// 风控拦截后不得再发 HTML 搜索页回退请求，避免继续踩验证页。
	for _, request := range fake.HTTPRequests {
		if strings.Contains(request.URL, "www.douyin.com/search/") {
			t.Fatalf("unexpected search page fallback after risk control: %s", request.URL)
		}
	}
}

func TestAddDouyinSubscriptionResolvesExactMatch(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{"sec_uid": "MS4wLjABAAAAone", "nickname": "测试用户", "unique_id": "testuser"},
	))}
	current := plugin.Settings{}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"测试用户"}},
	}}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, event, "douyin")
	if !outcome.Changed || outcome.User == nil || outcome.Action != "subscribed" {
		t.Fatalf("douyin subscription was not added: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 {
		t.Fatalf("subscriptions = %#v", current.Subscriptions)
	}
	item := current.Subscriptions[0]
	if item.Platform != "douyin" || item.UID != "MS4wLjABAAAAone" || item.Name != "测试用户" {
		t.Fatalf("subscription lost douyin identity: %#v", item)
	}
}

func TestAddDouyinSubscriptionOffersCandidates(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, douyinSearchDocument(
		map[string]any{"sec_uid": "MS4wLjABAAAAother", "nickname": "别的用户"},
	))}
	current := plugin.Settings{}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"测试用户"}},
	}}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, event, "douyin")
	if outcome.Changed || outcome.Action != "candidates" || len(outcome.Candidates) != 1 {
		t.Fatalf("ambiguous douyin query did not offer candidates: %#v", outcome)
	}
	if len(current.Subscriptions) != 0 {
		t.Fatalf("ambiguous query wrote a subscription: %#v", current.Subscriptions)
	}
}

func TestAddDouyinSubscriptionUsesExplicitSecUIDWithoutLookup(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	current := plugin.Settings{}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"MS4wLjABAAAAoffline"}},
	}}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, event, "douyin")
	if !outcome.Changed || outcome.User == nil || outcome.User.UID != "MS4wLjABAAAAoffline" {
		t.Fatalf("explicit sec_uid was not used directly: %#v", outcome)
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].UID != "MS4wLjABAAAAoffline" {
		t.Fatalf("offline subscription = %#v", current.Subscriptions)
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("offline fallback sent requests: %#v", testkit.RequestURLs(fake))
	}
}

func TestAddDouyinSubscriptionReusesKnownSourceWithoutLookup(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	current := plugin.Settings{Subscriptions: []plugin.Subscription{{
		ID: "known", Platform: "douyin", UID: "MS4wLjABAAAAknown", Name: "已知用户", AvatarURL: "https://p3-pc.douyinpic.com/known.jpeg",
		TargetType: "group", TargetID: "old", Services: []string{"all"}, Enabled: true,
	}}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "new"},
		Payload: map[string]any{"args": []string{"已知用户"}},
	}}
	outcome := newHandler(t).AddSubscription(context.Background(), fake, &current, event, "douyin")
	if !outcome.Changed || outcome.User == nil || outcome.User.UID != "MS4wLjABAAAAknown" {
		t.Fatalf("known source was not reused: %#v", outcome)
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("known source triggered upstream lookup: %#v", testkit.RequestURLs(fake))
	}
}

func TestDouyinManagementResolutionMatchesUniqueID(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPDefault = testkit.HTTPJSON(200, douyinSearchDocument(map[string]any{"sec_uid": "MS4wLjABAAAAone", "unique_id": "testuser", "nickname": "测试用户"}))
	handler := newHandler(t)
	result := handler.ResolveUser(t.Context(), fake, "douyin", "testuser", plugin.Settings{})
	if !plugin.BoolScalar(result["exact"]) {
		t.Fatalf("unique_id did not produce an exact match: %#v", result)
	}
	user, ok := result["user"].(douyinUser)
	if !ok || user.UID != "MS4wLjABAAAAone" {
		t.Fatalf("exact user = %#v", result["user"])
	}
	if unresolved := handler.ResolveUser(t.Context(), fake, "douyin", "不存在", plugin.Settings{}); plugin.BoolScalar(unresolved["exact"]) {
		t.Fatalf("unknown query resolved exactly: %#v", unresolved)
	}
}

func TestDouyinSecUIDFromInput(t *testing.T) {
	for input, want := range map[string]string{
		"MS4wLjABAAAAtest123":                                   "MS4wLjABAAAAtest123",
		"https://www.douyin.com/user/MS4wLjABAAAAtest123":       "MS4wLjABAAAAtest123",
		"看看 https://www.douyin.com/user/MS4wLjABAAAAtest123 这个": "MS4wLjABAAAAtest123",
		"https://example.com/user/MS4wLjABAAAAtest123":          "",
		"普通昵称": "",
	} {
		if got := douyinSecUIDFromInput(input); got != want {
			t.Fatalf("douyinSecUIDFromInput(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseDouyinPreviewURL(t *testing.T) {
	for rawURL, kind := range map[string]string{
		"https://www.douyin.com/video/7000000000000000000":           "video",
		"https://www.douyin.com/note/7000000000000000001":            "image_text",
		"https://www.iesdouyin.com/share/video/7000000000000000002/": "video",
		"https://v.douyin.com/abc123/":                               "short",
		"https://live.douyin.com/123456":                             "live",
	} {
		ref := parseDouyinPreviewURL(rawURL)
		if ref == nil || ref.Kind != kind {
			t.Fatalf("parseDouyinPreviewURL(%q) = %#v, want %s", rawURL, ref, kind)
		}
	}
	for _, rawURL := range []string{
		"https://www.bilibili.com/video/BV1xx411c7mD",
		"https://example.com/video/1",
		"https://www.douyin.com/",
		"不是链接",
	} {
		if ref := parseDouyinPreviewURL(rawURL); ref != nil {
			t.Fatalf("parseDouyinPreviewURL(%q) = %#v, want nil", rawURL, ref)
		}
	}
}

func TestDouyinRouterDataExtractsCurrentSharePageItemID(t *testing.T) {
	body := douyinRouterDataPage(map[string]any{"loaderData": map[string]any{
		"video_(id)/page": map[string]any{"itemId": "7679356419690253583"},
	}})
	if got := douyinPreviewIDFromPage(body); got != "7679356419690253583" {
		t.Fatalf("douyinPreviewIDFromPage() = %q", got)
	}
	if kind := douyinHTMLBlockKind(200, body); kind != "upstream" {
		t.Fatalf("router data page kind = %q", kind)
	}
}

func TestFetchDouyinPreviewUsesMobileShortLinkAndSignedDetailAPI(t *testing.T) {
	const awemeID = "7679356419690253583"
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.KV["source:douyin:web_cookies:primary"] = map[string]any{
		"msToken": "fixture-mstoken-primary", "ms_token_fetched_at": time.Now().Unix(),
	}
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/4ZDtWeIBr4g/", Result: rayleabot.ActionResult{
			"status_code": 302,
			"headers":     map[string]any{"Location": "https://www.iesdouyin.com/share/video/" + awemeID + "/"},
		}},
		{Path: "/share/video/" + awemeID + "/", Result: rayleabot.ActionResult{
			"status_code": 200,
			"headers":     map[string]any{"Content-Type": "text/html"},
			"body_text": douyinRouterDataPage(map[string]any{"loaderData": map[string]any{
				"video_(id)/page": map[string]any{"itemId": awemeID},
			}}),
		}},
		{Path: "/aweme/v1/web/aweme/detail/", Result: testkit.HTTPJSON(200, map[string]any{
			"status_code":  0,
			"aweme_detail": douyinVideoAweme(awemeID, "MS4wLjABAAAAfixture", "测试用户", "测试作品", time.Now().Unix()),
		})},
	}

	update, err := fetchDouyinPreview(context.Background(), fake, &douyinPreviewRef{
		Kind: "short", ID: "4ZDtWeIBr4g", URL: "https://v.douyin.com/4ZDtWeIBr4g/",
	})
	if err != nil {
		t.Fatalf("fetchDouyinPreview() error = %v", err)
	}
	if plugin.StringScalar(update["id"]) != awemeID || plugin.StringScalar(update["url"]) != "https://www.douyin.com/video/"+awemeID {
		t.Fatalf("preview update = %#v", update)
	}
	if len(fake.HTTPRequests) != 3 {
		t.Fatalf("preview requests = %#v", testkit.RequestURLs(fake))
	}
	for _, request := range fake.HTTPRequests[:2] {
		if request.Headers["User-Agent"] != douyinShareUserAgent {
			t.Fatalf("share request user agent = %q", request.Headers["User-Agent"])
		}
	}
	detailRequest := fake.HTTPRequests[2]
	parsed, parseErr := url.Parse(detailRequest.URL)
	if parseErr != nil {
		t.Fatalf("parse detail URL: %v", parseErr)
	}
	if parsed.Query().Get("aweme_id") != awemeID || parsed.Query().Get("a_bogus") == "" {
		t.Fatalf("detail request is not signed: %s", detailRequest.URL)
	}
	if detailRequest.Headers["User-Agent"] != douyinUserAgent || !strings.Contains(detailRequest.Headers["Cookie"], "sessionid=primary") {
		t.Fatalf("detail request headers = %#v", detailRequest.Headers)
	}
}

func TestFetchDouyinPreviewUsesCompleteSharePageBeforeAccountAPI(t *testing.T) {
	const awemeID = "7679356419690253583"
	fake := newActions()
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/share-fast/", Result: rayleabot.ActionResult{
			"status_code": 302,
			"headers":     map[string]any{"Location": "https://www.iesdouyin.com/share/video/" + awemeID + "/"},
		}},
		{Path: "/share/video/" + awemeID + "/", Result: rayleabot.ActionResult{
			"status_code": 200,
			"headers":     map[string]any{"Content-Type": "text/html"},
			"body_text": douyinRouterDataPage(map[string]any{"loaderData": map[string]any{
				"video_(id)/page": map[string]any{"aweme_detail": douyinVideoAweme(awemeID, "MS4wLjABAAAAfixture", "测试用户", "测试作品", time.Now().Unix())},
			}}),
		}},
	}

	update, err := fetchDouyinPreview(context.Background(), fake, &douyinPreviewRef{
		Kind: "short", ID: "share-fast", URL: "https://v.douyin.com/share-fast/",
	})
	if err != nil {
		t.Fatalf("fetchDouyinPreview() error = %v", err)
	}
	if plugin.StringScalar(update["id"]) != awemeID {
		t.Fatalf("share page update = %#v", update)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("complete share page still reached account API: %#v", testkit.RequestURLs(fake))
	}
}

func TestFetchDouyinPreviewAnonymousDetailWithoutAccounts(t *testing.T) {
	const awemeID = "7679356419690253583"
	fake := newActions()
	avatarURL := "https://p3-pc.douyinpic.com/aweme/100x100/aweme-avatar/fixture.jpeg?from=327834062"
	fake.HTTPRoutes = []testkit.HTTPRoute{
		{Path: "/4ZDtWeIBr4g/", Result: rayleabot.ActionResult{
			"status_code": 302,
			"headers":     map[string]any{"Location": "https://www.iesdouyin.com/share/video/" + awemeID + "/"},
		}},
		{Path: "/share/video/" + awemeID + "/", Result: rayleabot.ActionResult{
			"status_code": 200,
			"headers":     map[string]any{"Content-Type": "text/html"},
			"body_text": douyinRouterDataPage(map[string]any{"loaderData": map[string]any{
				"video_(id)/page": map[string]any{"itemId": awemeID},
			}}),
		}},
		{Path: "/aweme/v1/web/aweme/detail/", Result: testkit.HTTPJSON(200, map[string]any{
			"status_code": 0,
			"aweme_detail": map[string]any{
				"aweme_id": awemeID, "aweme_type": 0, "desc": "匿名链作品", "create_time": time.Now().Unix(),
				"author": map[string]any{
					"sec_uid": "MS4wLjABAAAAfixture", "nickname": "测试用户",
					"avatar_thumb": map[string]any{"url_list": []any{avatarURL}},
				},
				"video": map[string]any{
					"duration": 80000,
					"cover":    map[string]any{"url_list": []any{"https://p3-pc-sign.douyinpic.com/fixture-cover.jpeg"}},
				},
			},
		})},
	}

	update, err := fetchDouyinPreview(context.Background(), fake, &douyinPreviewRef{
		Kind: "short", ID: "4ZDtWeIBr4g", URL: "https://v.douyin.com/4ZDtWeIBr4g/",
	})
	if err != nil {
		t.Fatalf("fetchDouyinPreview() error = %v", err)
	}
	if plugin.StringScalar(update["id"]) != awemeID {
		t.Fatalf("anonymous detail update = %#v", update)
	}
	author := plugin.MapValue(update["author"])
	if plugin.StringScalar(author["avatar"]) != avatarURL {
		t.Fatalf("anonymous detail avatar = %#v", author["avatar"])
	}
	for _, request := range fake.HTTPRequests[:2] {
		if !strings.Contains(request.Headers["Cookie"], "ttwid=") {
			t.Fatalf("share request missing anonymous ttwid: %#v", request.Headers)
		}
	}
	detailRequest := fake.HTTPRequests[2]
	if !strings.Contains(detailRequest.Headers["Cookie"], "ttwid=fixture-ttwid") || strings.Contains(detailRequest.Headers["Cookie"], "sessionid") {
		t.Fatalf("anonymous detail request cookie = %q", detailRequest.Headers["Cookie"])
	}
	parsed, parseErr := url.Parse(detailRequest.URL)
	if parseErr != nil {
		t.Fatalf("parse anonymous detail URL: %v", parseErr)
	}
	if parsed.Query().Get("a_bogus") != "" {
		t.Fatalf("anonymous detail request must stay unsigned: %s", detailRequest.URL)
	}
}

func TestAnonymousTTWIDRegistersAndPersistsOnce(t *testing.T) {
	fake := testkit.NewActions()
	client := newDouyinClient(fake)
	fake.HTTPRoutes = []testkit.HTTPRoute{{
		Path: "/ttwid/union/register/", Result: rayleabot.ActionResult{
			"status_code": 200,
			"headers":     map[string]any{"Set-Cookie": "ttwid=registered-ttwid; Path=/; Domain=bytedance.com"},
		},
	}}
	first := client.anonymousTTWID(context.Background())
	if first != "registered-ttwid" {
		t.Fatalf("anonymousTTWID() = %q", first)
	}
	if _, exists := fake.KV["source:douyin:anon_ttwid"]; !exists {
		t.Fatalf("registered ttwid was not persisted to KV: %#v", fake.KV)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("register requests = %#v", testkit.RequestURLs(fake))
	}
	if second := client.anonymousTTWID(context.Background()); second != "registered-ttwid" || len(fake.HTTPRequests) != 1 {
		t.Fatalf("second anonymousTTWID() = %q with %d requests", second, len(fake.HTTPRequests))
	}
}

func TestValidateAvatarSourceURLAllowsDouyinCDN(t *testing.T) {
	for _, sourceURL := range []string{
		"https://p3-pc.douyinpic.com/aweme/100x100/face.jpeg",
		"https://p3-sign.douyinpic.com/obj/fixture.jpeg?Expires=1780905600&ssig=abc",
		"https://douyinpic.com/face.jpeg",
	} {
		if _, referer, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy()); err != nil || referer != "https://www.douyin.com/" {
			t.Fatalf("validateAvatarSourceURL(%q) = referer %q, err %v", sourceURL, referer, err)
		}
	}
	for _, sourceURL := range []string{
		"https://douyinpic.com.evil.example/face.jpeg",
		"https://notdouyinpic.com/face.jpeg",
		"http://p3-pc.douyinpic.com/face.jpeg",
	} {
		if _, _, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy()); err == nil {
			t.Fatalf("validateAvatarSourceURL(%q) accepted an undeclared host", sourceURL)
		}
	}
}

func TestInlineDouyinUpdateAvatarUsesSlowCDNBudget(t *testing.T) {
	fake := newActions()
	fake.HTTPDefault = testkit.AvatarHTTPResult()
	data := map[string]any{"author": map[string]any{
		"name": "测试用户", "avatar": "https://p3-pc.douyinpic.com/aweme/100x100/face.jpeg",
	}}

	newHandler(t).InlineUpdateAvatars(context.Background(), fake, data)

	if len(fake.HTTPRequests) != 1 || fake.HTTPRequests[0].TimeoutSeconds != plugin.SlowAvatarTimeoutSeconds {
		t.Fatalf("unexpected Douyin avatar request budget: %#v", fake.HTTPRequests)
	}
}

func TestPrepareDouyinUpdateResourcesAssignsRenderResources(t *testing.T) {
	data := map[string]any{"media_items": []map[string]any{
		{"url": "https://p3-pc.douyinpic.com/aweme/cover.jpeg", "fallback": "assets/cover.svg"},
		{"url": "https://example.com/evil.jpeg", "fallback": "assets/grid.svg"},
	}}
	resources := prepareDouyinUpdateResources(data)
	if len(resources) != 1 || resources[0].URL != "https://p3-pc.douyinpic.com/aweme/cover.jpeg" || resources[0].Referer != "https://www.douyin.com/" {
		t.Fatalf("unexpected douyin resources: %#v", resources)
	}
	items := plugin.MapSliceValue(data["media_items"])
	if plugin.StringScalar(items[0]["resource_id"]) != "douyin-media-0" {
		t.Fatalf("trusted media lost its resource id: %#v", items[0])
	}
	if plugin.StringScalar(items[1]["url"]) != "assets/grid.svg" || plugin.StringScalar(items[1]["resource_id"]) != "" {
		t.Fatalf("untrusted media was not replaced by fallback: %#v", items[1])
	}
}

func TestDouyinUserFromPageParsesRenderData(t *testing.T) {
	page := douyinRenderDataPage(map[string]any{"user": map[string]any{
		"sec_uid": "MS4wLjABAAAAone", "nickname": "页面用户", "unique_id": "pageuser",
	}})
	user := douyinUserFromPage(page)
	if user.UID != "MS4wLjABAAAAone" || user.Name != "页面用户" || user.UniqueID != "pageuser" {
		t.Fatalf("page user = %#v", user)
	}
}

func TestDouyinDiagnosticsNeverIncludeUpstreamBody(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 403,
		"body_text":   `{"status_msg":"blocked sessionid=private-value; sid_guard=masked-value"}`,
	}}
	client := newDouyinClient(fake)
	_, err := client.requestJSON(context.Background(), douyinAwemePostAPIURL("MS4wLjABAAAAfixture"), douyinAccount{ID: "primary", Cookie: "sessionid=fixture; ttwid=fixture-ttwid; msToken=fixture-mstoken; webid=7000000000000000001;"}, douyinWebReferer)
	if err == nil {
		t.Fatal("requestJSON error = nil")
	}
	message := err.Error() + " " + friendlyDouyinSourceError("抖音检查失败", err)
	for _, secret := range []string{"private-value", "masked-value", "sessionid=", "sid_guard="} {
		if strings.Contains(message, secret) {
			t.Fatalf("douyin diagnostic leaked %q: %s", secret, message)
		}
	}
	if !strings.Contains(message, "HTTP 403") {
		t.Fatalf("douyin diagnostic = %q", message)
	}
	var sourceErr *douyinSourceError
	if !errors.As(err, &sourceErr) || sourceErr.Kind != "session_blocked" {
		t.Fatalf("HTTP 403 classification = %#v", sourceErr)
	}
	if strings.Contains(message, "CK 已失效") || !strings.Contains(message, "服务器检查结果") {
		t.Fatalf("HTTP 403 feedback = %q", message)
	}
	if len(fake.AccountValidations) != 1 || fake.AccountValidations[0].Observation != "session_blocked" || fake.AccountValidations[0].HTTPStatus != 403 {
		t.Fatalf("HTTP 403 validation request = %#v", fake.AccountValidations)
	}
}

func TestCommandOperationCoversDouyinSearch(t *testing.T) {
	if platform, operation := newHandler(t).CommandOperation("抖音搜索用户"); platform != "douyin" || operation != "search" {
		t.Fatalf("douyin search command is not wired: %q %q", platform, operation)
	}
}

func TestDouyinSearchPausesAfterEmptyAPIAndPageResults(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{
		testkit.HTTPJSON(200, douyinSearchDocument()),
		{"status_code": 200, "body_text": `<html><body><nav>登录</nav><script id="RENDER_DATA" type="application/json">{"loaderData":{"search":{"data":[]}}}</script></body></html>`},
	}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err == nil || len(users) != 0 {
		t.Fatalf("empty search users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 3 {
		t.Fatalf("search should use one API request and one page fallback: %#v", testkit.RequestURLs(fake))
	}
	message := err.Error()
	if !strings.Contains(message, "主页链接") || !strings.Contains(message, "sec_uid") || strings.Contains(message, "重新扫码") {
		t.Fatalf("empty search guidance = %q", message)
	}
	if _, exists := fake.KV[douyinSearchPauseKey]; !exists {
		t.Fatalf("empty search did not open a pause: %#v", fake.KV)
	}

	fake.HTTPRequests = nil
	if _, err = searchDouyinWithActions(context.Background(), fake, "另一个用户"); err == nil || !strings.Contains(err.Error(), "已暂停") {
		t.Fatalf("paused search error = %v", err)
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("paused search still reached upstream: %#v", testkit.RequestURLs(fake))
	}
}

func TestDouyinSearchPauseAllowsHostResolve(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.KV[douyinSearchPauseKey] = map[string]any{
		"kind": "risk_control", "until": time.Now().Add(10 * time.Minute).Unix(),
	}
	fake.ResolveResults = []rayleabot.ActionResult{{
		"platform": "douyin",
		"profiles": []any{
			map[string]any{"uid": "MS4wLjABAAAAhost", "nickname": "测试用户", "avatar_url": "https://p3-pc.douyinpic.com/host.jpeg"},
		},
		"exact": true,
	}}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err != nil || len(users) != 1 || users[0].UID != "MS4wLjABAAAAhost" {
		t.Fatalf("paused host resolve users=%#v, err=%v", users, err)
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("paused resolve still reached upstream: %#v", testkit.RequestURLs(fake))
	}
	if _, exists := fake.KV[douyinSearchPauseKey]; exists {
		t.Fatalf("successful paused resolve left a pause behind: %#v", fake.KV)
	}
}

func TestDouyinSearchPauseKeptWhenHostResolveEmpty(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.KV[douyinSearchPauseKey] = map[string]any{
		"kind": "risk_control", "until": time.Now().Add(10 * time.Minute).Unix(),
	}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err == nil || !strings.Contains(err.Error(), "已暂停") {
		t.Fatalf("paused empty resolve err=%v", err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %#v, want none", users)
	}
	if len(fake.ResolveRequests) != 1 {
		t.Fatalf("host resolve was not attempted: %#v", fake.ResolveRequests)
	}
	if _, exists := fake.KV[douyinSearchPauseKey]; !exists {
		t.Fatalf("failed resolve dropped the pause: %#v", fake.KV)
	}
}

func TestDouyinSearchFallsBackToHostResolveWhenIntercepted(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
		"status_code": 0,
		"user_list":   []any{},
		"search_nil_info": map[string]any{
			"search_nil_type": "verify_check",
		},
	})}
	fake.ResolveResults = []rayleabot.ActionResult{{
		"platform": "douyin",
		"profiles": []any{
			map[string]any{"uid": "MS4wLjABAAAAhost", "unique_id": "host_douyin_id", "nickname": "测试用户", "avatar_url": "https://p3-pc.douyinpic.com/host.jpeg"},
		},
		"exact": true,
	}}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err != nil || len(users) != 1 {
		t.Fatalf("host-resolved search users=%#v, err=%v", users, err)
	}
	if users[0].UID != "MS4wLjABAAAAhost" || users[0].UniqueID != "host_douyin_id" || users[0].Name != "测试用户" || users[0].AvatarURL != "https://p3-pc.douyinpic.com/host.jpeg" {
		t.Fatalf("host-resolved profile lost fields: %#v", users[0])
	}
	if len(fake.ResolveRequests) != 1 || fake.ResolveRequests[0].Platform != "douyin" || fake.ResolveRequests[0].Query != "测试用户" {
		t.Fatalf("host resolve request = %#v", fake.ResolveRequests)
	}
	if want := "sessionid=primary; ttwid=fixture-ttwid-primary; msToken=fixture-mstoken-primary; webid=7000000000000000001; s_v_web_id=verify_fixture_primary;"; fake.ResolveRequests[0].Cookie != want {
		t.Fatalf("host resolve cookie = %q, want %q", fake.ResolveRequests[0].Cookie, want)
	}
	if _, exists := fake.KV[douyinSearchPauseKey]; exists {
		t.Fatalf("successful host resolve left a pause behind: %#v", fake.KV)
	}
}

func TestDouyinSearchKeepsRiskErrorWhenHostResolveEmpty(t *testing.T) {
	fake := newActions()
	fake.Accounts = fixtureDouyinAccounts("primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
		"status_code": 0,
		"user_list":   []any{},
		"search_nil_info": map[string]any{
			"search_nil_type": "verify_check",
		},
	})}
	users, err := searchDouyinWithActions(context.Background(), fake, "测试用户")
	if err == nil || !strings.Contains(err.Error(), "安全验证") {
		t.Fatalf("empty host resolve users=%#v, err=%v", users, err)
	}
	if len(users) != 0 {
		t.Fatalf("users = %#v, want none", users)
	}
	if len(fake.ResolveRequests) != 1 {
		t.Fatalf("host resolve was not attempted: %#v", fake.ResolveRequests)
	}
	if _, exists := fake.KV[douyinSearchPauseKey]; !exists {
		t.Fatalf("risk control did not open a pause: %#v", fake.KV)
	}
}

func TestDouyinHTMLBlockKindRequiresExplicitInterstitial(t *testing.T) {
	normal := `<html><body><nav>登录</nav><script>const captchaLabel = "captcha";</script><script id="RENDER_DATA" type="application/json">{}</script></body></html>`
	if kind := douyinHTMLBlockKind(200, normal); kind != "upstream" {
		t.Fatalf("normal page kind = %q", kind)
	}
	for _, blocked := range []string{
		`<html><head><title>验证码中间页</title></head></html>`,
		`<html><body><div>为了你的账号安全，请先完成验证</div></body></html>`,
	} {
		if kind := douyinHTMLBlockKind(200, blocked); kind != "risk_control" {
			t.Fatalf("blocked page kind = %q", kind)
		}
	}
	shell := `<html><body><script>window._$jsvmprt = function () {};</script></body></html>`
	if kind := douyinHTMLBlockKind(200, shell); kind != "upstream" {
		t.Fatalf("generic shell kind = %q", kind)
	}
}
