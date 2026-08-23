package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type subscriptionCheckDeadlineActions struct {
	*fakePluginActions
	deadline         time.Time
	hasDeadline      bool
	stateDeadline    time.Time
	hasStateDeadline bool
}

func (actions *subscriptionCheckDeadlineActions) ThirdPartyAccountRead(ctx context.Context, request rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error) {
	actions.deadline, actions.hasDeadline = ctx.Deadline()
	return actions.fakePluginActions.ThirdPartyAccountRead(ctx, request)
}

func (actions *subscriptionCheckDeadlineActions) KVSet(ctx context.Context, key string, value any) (rayleabot.ActionResult, error) {
	actions.stateDeadline, actions.hasStateDeadline = ctx.Deadline()
	return actions.fakePluginActions.KVSet(ctx, key, value)
}

type cancelAfterFirstWeiboRequestActions struct {
	*fakePluginActions
	cancel       context.CancelFunc
	requestCount int
}

func (actions *cancelAfterFirstWeiboRequestActions) HTTPRequest(ctx context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	result, err := actions.fakePluginActions.HTTPRequest(ctx, request)
	actions.requestCount++
	if actions.requestCount == 1 {
		actions.cancel()
	}
	return result, err
}

func (actions *cancelAfterFirstWeiboRequestActions) KVSet(ctx context.Context, key string, value any) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return actions.fakePluginActions.KVSet(ctx, key, value)
}

func TestWeiboFeedPollsEachSubjectIndependently(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{
		{
			path: "/api/container/getIndex", queryContains: "containerid=1076036000000001",
			result: weiboFeedResult(
				weiboTextMblog("mid-1", "6000000001", "第一位博主的微博", 1700000000),
				weiboTextMblog("mid-other", "6999999999", "未订阅账号", 1700000090),
			),
		},
		{
			path: "/api/container/getIndex", queryContains: "containerid=1076036000000002",
			result: weiboFeedResult(weiboTextMblog("mid-2", "6000000002", "第二位博主的微博", 1700000060)),
		},
	}
	result := newWeiboSource(fake).poll(context.Background(), []subscription{
		{ID: "one", Platform: "weibo", UID: "6000000001", Services: []string{"post"}, Enabled: true},
		{ID: "two", Platform: "weibo", UID: "6000000002", Services: []string{"post"}, Enabled: true},
	})
	if !result.FeedOK || len(result.Updates) != 2 {
		t.Fatalf("multi-subject result = %#v", result)
	}
	if countHTTPByPath(fake, "/api/container/getIndex") != 2 {
		t.Fatalf("per-user feed requests = %#v", requestURLs(fake))
	}
	for _, request := range fake.httpRequests {
		if strings.Contains(request.URL, "containerid=102803") {
			t.Fatalf("popular recommendations endpoint was used: %s", request.URL)
		}
	}
}

func TestWeiboFeedFollowsCursorUntilDeliveryAgeBoundary(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{ID: "paged-weibo", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true}
	fake.kv[weiboFeedSourceKey(item)] = true
	fake.httpResponses = []rayleabot.ActionResult{
		weiboFeedPageResult("next-page", weiboTextMblog("new-1", item.UID, "第一页微博", now.Unix())),
		weiboFeedPageResult("ignored-page", weiboTextMblog("old", item.UID, "超过投递时效", now.Add(-31*time.Minute).Unix())),
	}

	result := newWeiboSource(fake).pollSince(context.Background(), []subscription{item}, now.Add(-30*time.Minute))

	if !result.FeedOK || len(result.Updates) != 2 {
		t.Fatalf("paged feed result = %#v", result)
	}
	if len(fake.httpRequests) != 2 || !strings.Contains(fake.httpRequests[1].URL, "since_id=next-page") {
		t.Fatalf("feed cursor was not followed: %#v", requestURLs(fake))
	}
}

func TestWeiboFeedDoesNotTreatSeenUpdateAsPagingBoundary(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	first := subscription{ID: "paged-first", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true}
	fake.kv[weiboFeedSourceKey(first)] = true
	boundary := normalizeWeiboMblog(weiboTextMblog("boundary", first.UID, "部分目标已推送", now.Add(-time.Minute).Unix()), 0)
	fake.kv[subscriptionUpdateKey(first, boundary)] = true
	fake.httpResponses = []rayleabot.ActionResult{
		weiboFeedPageResult("next-page", weiboTextMblog("boundary", first.UID, "部分目标已推送", now.Add(-time.Minute).Unix())),
		weiboFeedPageResult("", weiboTextMblog("pending", first.UID, "仍需检查的微博", now.Add(-2*time.Minute).Unix())),
	}

	result := newWeiboSource(fake).pollSince(context.Background(), []subscription{first}, now.Add(-30*time.Minute))

	if !result.FeedOK || len(result.Updates) != 2 || len(fake.httpRequests) != 2 {
		t.Fatalf("feed stopped at a partially seen boundary: result=%#v requests=%#v", result, requestURLs(fake))
	}
}

func TestWeiboSourceUsesPerUserFeedWithoutFollowMutation(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpRoutes = []fakeHTTPRoute{{
		path: "/api/container/getIndex", queryContains: "containerid=1076036000000001", result: weiboFeedResult(),
	}}
	result := newWeiboSource(fake).poll(context.Background(), []subscription{
		{ID: "one", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true},
	})
	if !result.FeedOK {
		t.Fatalf("per-user poll failed: %#v", result)
	}
	if len(fake.httpRequests) != 1 || fake.httpRequests[0].Method != "GET" {
		t.Fatalf("unexpected requests: %#v", fake.httpRequests)
	}
	parsed, err := url.Parse(fake.httpRequests[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("containerid") != "1076036000000001" || parsed.Query().Get("value") != "6000000001" {
		t.Fatalf("per-user feed query = %s", parsed.RawQuery)
	}
}

func TestWeiboFeedPageLimitIsIncomplete(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{ID: "busy", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true}
	fake.kv[weiboFeedSourceKey(item)] = true
	for page := 0; page < weiboFeedMaxPages; page++ {
		fake.httpResponses = append(fake.httpResponses, weiboFeedPageResult(
			fmt.Sprintf("page-%d", page+2),
			weiboTextMblog(fmt.Sprintf("mid-%d", page), item.UID, "高频更新", now.Add(-time.Duration(page)*time.Minute).Unix()),
		))
	}
	result := newWeiboSource(fake).pollSince(context.Background(), []subscription{item}, now.Add(-30*time.Minute))
	if result.FeedOK || !result.ReadyUIDs[item.UID] || len(result.Errors) == 0 {
		t.Fatalf("page-limited feed was reported complete: %#v", result)
	}
	resumeKey := newWeiboSource(fake).feedResumeKey(item.UID)
	if stringScalar(nestedValue(fake.kv[resumeKey], "cursor")) != "page-6" {
		t.Fatalf("page-limited feed did not save its continuation: %#v", fake.kv[resumeKey])
	}

	fake.httpRequests = nil
	fake.httpResponses = []rayleabot.ActionResult{
		weiboFeedPageResult("fresh-page-2", weiboTextMblog("fresh", item.UID, "最新微博", now.Add(time.Minute).Unix())),
		weiboFeedPageResult("fresh-page-3", weiboTextMblog("fresh-page-two", item.UID, "次新微博", now.Unix())),
		weiboFeedResult(weiboTextMblog("resumed", item.UID, "续页微博", now.Add(-5*time.Minute).Unix())),
	}
	resumed := newWeiboSource(fake).pollSince(context.Background(), []subscription{item}, now.Add(-30*time.Minute))
	if !resumed.FeedOK || len(resumed.Updates) != 3 || len(fake.httpRequests) != 3 || !strings.Contains(fake.httpRequests[2].URL, "since_id=page-6") {
		t.Fatalf("saved continuation was not resumed: result=%#v requests=%#v", resumed, requestURLs(fake))
	}
	if _, exists := fake.kv[resumeKey]; exists {
		t.Fatalf("completed continuation was not cleared: %#v", fake.kv[resumeKey])
	}
}

func TestWeiboSourceRotatesAccountsAfterRiskControl(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary", "backup")
	now := time.Unix(1780905600, 0)
	fake.httpResponses = []rayleabot.ActionResult{
		httpJSONResult(412, map[string]any{"ok": 0, "msg": "请求被拦截"}),
		weiboFeedResult(),
	}
	source := newWeiboSource(fake)
	source.client.now = func() time.Time { return now }
	result := source.poll(context.Background(), []subscription{{
		ID: "weibo-one", Platform: "weibo", UID: "6000000001", Services: []string{"post"}, Enabled: true,
	}})
	if !result.FeedOK || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "风控") {
		t.Fatalf("unexpected source result: %#v", result)
	}
	if len(fake.httpRequests) != 2 || !strings.Contains(fake.httpRequests[0].Headers["Cookie"], "primary") || !strings.Contains(fake.httpRequests[1].Headers["Cookie"], "backup") {
		t.Fatalf("account rotation requests = %#v", fake.httpRequests)
	}
	if _, exists := fake.kv["source:weibo:cooldown:feed:primary"]; !exists {
		t.Fatalf("risk-controlled account did not enter cooldown: %#v", fake.kv)
	}
	if len(fake.logs) == 0 || fake.logs[0].Message != "微博订阅源检查失败" {
		t.Fatalf("risk-control failure was not logged: %#v", fake.logs)
	}
}

func TestWeiboSourceClassifiesHTTP432AndEntersCooldown(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpResponses = []rayleabot.ActionResult{{
		"status_code": 432,
		"body_text":   `{"ok":0,"msg":"fixture-upstream-body SUB=fixture-leak;"}`,
	}}
	now := time.Unix(1787274976, 0)
	source := newWeiboSource(fake)
	source.client.now = func() time.Time { return now }
	item := subscription{ID: "weibo-432", Platform: "weibo", UID: "6000000001", Services: []string{"post"}, Enabled: true}

	result := source.poll(context.Background(), []subscription{item})

	if result.FeedOK || len(result.Errors) != 1 || result.Errors[0] != "微博检查失败：H5 会话被拒绝（HTTP 432），可能是 CK 失效或平台风控；请在三方账号页检查 CK。" {
		t.Fatalf("HTTP 432 result = %#v", result)
	}
	if _, exists := fake.kv["source:weibo:cooldown:feed:primary"]; !exists {
		t.Fatalf("HTTP 432 did not enter cooldown: %#v", fake.kv)
	}
	if len(fake.logs) != 1 {
		t.Fatalf("HTTP 432 logs = %#v", fake.logs)
	}
	fields := fake.logs[0].Fields
	if stringScalar(fields["kind"]) != "session_blocked" || intScalar(fields["http_status"]) != 432 || stringScalar(fields["uid"]) != item.UID || intScalar(fields["page"]) != 1 {
		t.Fatalf("HTTP 432 log fields = %#v", fields)
	}
	if _, exists := fields["error"]; exists {
		t.Fatalf("HTTP 432 log included a free-form error: %#v", fields)
	}
	if rendered := fmt.Sprint(fields); strings.Contains(rendered, "fixture-upstream-body") || strings.Contains(rendered, "fixture-leak") {
		t.Fatalf("HTTP 432 log included upstream response content: %#v", fields)
	}
	if len(fake.accountValidations) != 1 {
		t.Fatalf("HTTP 432 validation requests = %#v", fake.accountValidations)
	}
	validation := fake.accountValidations[0]
	if validation.Platform != "weibo" || validation.AccountID != "primary" || validation.Observation != "session_blocked" || validation.HTTPStatus != 432 {
		t.Fatalf("HTTP 432 validation request = %#v", validation)
	}

	second := source.poll(context.Background(), []subscription{item})
	if len(fake.httpRequests) != 1 || second.FeedOK || len(second.Errors) != 1 || !strings.Contains(second.Errors[0], "H5 会话阻断") {
		t.Fatalf("cooldown did not suppress the next request: result=%#v requests=%#v", second, fake.httpRequests)
	}
	if len(fake.accountValidations) != 1 {
		t.Fatalf("cooldown emitted another validation request: %#v", fake.accountValidations)
	}
}

func TestWeiboClientRequestsValidationForAuthenticationRejection(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{{"status_code": 401, "body_text": ""}}
	client := newWeiboClient(fake)

	_, err := client.requestJSON(context.Background(), weiboUserFeedURL("6000000001", ""), weiboAccount{ID: "primary", Cookie: "SUB=fixture;"}, weiboMobileReferer)
	var sourceErr *weiboSourceError
	if !errors.As(err, &sourceErr) || sourceErr.Kind != "auth" {
		t.Fatalf("authentication error = %#v", err)
	}
	if len(fake.accountValidations) != 1 {
		t.Fatalf("authentication validation requests = %#v", fake.accountValidations)
	}
	validation := fake.accountValidations[0]
	if validation.Platform != "weibo" || validation.AccountID != "primary" || validation.Observation != "auth_rejected" || validation.HTTPStatus != 401 {
		t.Fatalf("authentication validation request = %#v", validation)
	}
}

func TestWeiboSourceReportsStructuredUpstreamFailureInSummary(t *testing.T) {
	source := newWeiboSource(newFakePluginActions())
	message := source.friendlyError(&weiboSourceError{
		Kind:       "upstream",
		HTTPStatus: 500,
	})
	if message != "微博检查失败：上游请求异常（upstream，HTTP 500）。" {
		t.Fatalf("upstream summary = %q", message)
	}
}

func TestWeiboSubscriptionCheckBaselinesThenRendersNewMblog(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{
		ID: "weibo-6000000001-group-10000", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "group", TargetID: "10000", Services: []string{"post"}, Enabled: true,
		Subscribers: []subscriber{{ID: "nickname-only", Nickname: "订阅人"}},
	}
	current := settings{Enabled: true, Subscriptions: []subscription{item}}
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult(weiboTextMblog("old", "6000000001", "已有微博", now.Add(-time.Minute).Unix()))}
	first := checkSubscriptionsWithActionsAt(context.Background(), fake, current, now)
	if intScalar(first["sent"]) != 0 || !weiboFeedInitialized(context.Background(), fake, item) {
		t.Fatalf("first check did not establish baseline: result=%#v kv=%#v", first, fake.kv)
	}
	if stringScalar(first["skipped"]) != "" {
		t.Fatalf("weibo-only check was skipped: %#v", first)
	}
	if len(fake.renders) != 0 || len(fake.messages) != 0 {
		t.Fatalf("baseline emitted historical content")
	}

	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult(
		weiboTextMblog("new", "6000000001", "新微博", now.Add(time.Minute).Unix()),
		weiboTextMblog("old", "6000000001", "已有微博", now.Add(-time.Minute).Unix()),
	)}
	second := checkSubscriptionsWithActionsAt(context.Background(), fake, current, now.Add(2*time.Minute))
	if intScalar(second["sent"]) != 1 || boolScalar(second["degraded"]) {
		t.Fatalf("second check result = %#v", second)
	}
	if len(fake.renders) != 1 || fake.renders[0].Template != "weibo-update" {
		t.Fatalf("unexpected renders = %#v", fake.renders)
	}
	if stringScalar(fake.renders[0].Data["title"]) != "" || fake.renders[0].Data["content_text"] != "新微博" || fake.renders[0].Data["service"] != "文字" || fake.renders[0].Data["source_label"] != "文字微博" {
		t.Fatalf("rich render data was not restored: %#v", fake.renders[0].Data)
	}
	if _, exists := fake.kv["seen:weibo-6000000001-group-10000:post:new"]; !exists {
		t.Fatalf("new update was not marked seen")
	}
}

func TestWeiboSubscriptionCheckBaselineBlocksLaterHistoricalPages(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{ID: "new-busy", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true}
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedPageResult(
		"more-history", weiboTextMblog("latest", item.UID, "当前微博", now.Unix()),
	)}

	result := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{
		Enabled: true, Subscriptions: []subscription{item},
	}, now)
	initialized, baselineAt := weiboFeedState(context.Background(), fake, item)
	if boolScalar(result["degraded"]) || !initialized || !baselineAt.Equal(now) || len(fake.httpRequests) != 1 {
		t.Fatalf("new user feed did not baseline from its current page: result=%#v kv=%#v requests=%#v", result, fake.kv, requestURLs(fake))
	}

	fake.httpRequests = nil
	fake.httpResponses = []rayleabot.ActionResult{
		weiboFeedPageResult("more-history", weiboTextMblog("latest", item.UID, "当前微博", now.Unix())),
		weiboFeedResult(weiboTextMblog("history", item.UID, "订阅前历史微博", now.Add(-5*time.Minute).Unix())),
	}
	second := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{
		Enabled: true, Subscriptions: []subscription{item},
	}, now.Add(2*time.Minute))
	if intScalar(second["sent"]) != 0 || boolScalar(second["degraded"]) {
		t.Fatalf("later historical page escaped the baseline: %#v", second)
	}
	if len(fake.renders) != 0 || len(fake.messages) != 0 {
		t.Fatalf("later historical page was delivered: renders=%d messages=%d", len(fake.renders), len(fake.messages))
	}
	if _, exists := fake.kv["seen:new-busy:post:history"]; !exists {
		t.Fatalf("later historical page was not recorded as seen: %#v", fake.kv)
	}
}

func TestSubscriptionCheckAppliesTotalDeadline(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult()}
	actions := &subscriptionCheckDeadlineActions{fakePluginActions: fake}
	startedAt := time.Now()

	result := checkSubscriptionsWithActions(context.Background(), actions, settings{
		Enabled: true,
		Subscriptions: []subscription{{
			ID: "deadline", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true,
		}},
	})

	if boolScalar(result["degraded"]) || !actions.hasDeadline || !actions.hasStateDeadline {
		t.Fatalf("subscription check deadline result=%#v check_deadline=%v state_deadline=%v", result, actions.hasDeadline, actions.hasStateDeadline)
	}
	remaining := actions.deadline.Sub(startedAt)
	if remaining < subscriptionCheckTimeout-time.Second || remaining > subscriptionCheckTimeout+time.Second {
		t.Fatalf("subscription check deadline = %s, want about %s", remaining, subscriptionCheckTimeout)
	}
	stateRemaining := actions.stateDeadline.Sub(startedAt)
	totalTimeout := subscriptionCheckTimeout + subscriptionCheckFinalizationReserve
	if stateRemaining < totalTimeout-time.Second || stateRemaining > totalTimeout+time.Second {
		t.Fatalf("subscription check state deadline = %s, want about %s", stateRemaining, totalTimeout)
	}
}

func TestSubscriptionCheckReservesParentDeadlineForFinalization(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult()}
	actions := &subscriptionCheckDeadlineActions{fakePluginActions: fake}
	parentCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	parentDeadline, _ := parentCtx.Deadline()

	result := checkSubscriptionsWithActions(parentCtx, actions, settings{
		Enabled: true,
		Subscriptions: []subscription{{
			ID: "parent-deadline", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true,
		}},
	})

	if boolScalar(result["degraded"]) || !actions.hasDeadline || !actions.hasStateDeadline {
		t.Fatalf("subscription check parent deadline result=%#v check_deadline=%v state_deadline=%v", result, actions.hasDeadline, actions.hasStateDeadline)
	}
	want := parentDeadline.Add(-subscriptionCheckFinalizationReserve)
	drift := actions.deadline.Sub(want)
	if drift < -time.Second || drift > time.Second {
		t.Fatalf("subscription check deadline drift = %s, want deadline near %s", drift, want)
	}
	stateDrift := actions.stateDeadline.Sub(parentDeadline)
	if stateDrift < -time.Second || stateDrift > time.Second {
		t.Fatalf("subscription check state deadline drift = %s, want deadline near %s", stateDrift, parentDeadline)
	}
}

func TestWeiboFeedSavesContinuationWhenCheckIsInterrupted(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{ID: "interrupted", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true}
	fake.kv[weiboFeedSourceKey(item)] = true
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedPageResult(
		"next-page", weiboTextMblog("latest", item.UID, "最新微博", now.Unix()),
	)}
	checkCtx, cancel := context.WithCancel(context.Background())
	actions := &cancelAfterFirstWeiboRequestActions{fakePluginActions: fake, cancel: cancel}

	result := newWeiboSource(actions).pollSinceWithStateContext(
		checkCtx, context.Background(), []subscription{item}, now.Add(-30*time.Minute),
	)

	if result.FeedOK || !result.ReadyUIDs[item.UID] || len(fake.httpRequests) != 1 {
		t.Fatalf("interrupted feed result=%#v requests=%#v", result, requestURLs(fake))
	}
	if !strings.Contains(strings.Join(result.Errors, "\n"), subscriptionCheckIncompleteMessage) {
		t.Fatalf("interrupted feed errors = %#v", result.Errors)
	}
	resumeKey := newWeiboSource(fake).feedResumeKey(item.UID)
	if stringScalar(nestedValue(fake.kv[resumeKey], "cursor")) != "next-page" {
		t.Fatalf("interrupted feed did not save its continuation: %#v", fake.kv[resumeKey])
	}
}

func TestWeiboSubscriptionCheckExpiresStaleMblog(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{
		ID: "stale-weibo", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "private", TargetID: "20000", Services: []string{"post"}, Enabled: true,
	}
	fake.kv[weiboFeedSourceKey(item)] = true
	current := settings{Enabled: true, DeliveryMaxAgeMinutes: 5, Subscriptions: []subscription{item}}
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult(weiboTextMblog(
		"delayed", "6000000001", "过期微博", now.Add(-6*time.Minute).Unix(),
	))}
	result := checkSubscriptionsWithActionsAt(context.Background(), fake, current, now)
	if intScalar(result["sent"]) != 0 || boolScalar(result["degraded"]) {
		t.Fatalf("expired weibo result = %#v", result)
	}
	if len(fake.renders) != 0 || len(fake.messages) != 0 {
		t.Fatalf("expired weibo was delivered: renders=%d messages=%d", len(fake.renders), len(fake.messages))
	}
	if _, exists := fake.kv["seen:stale-weibo:post:delayed"]; !exists {
		t.Fatalf("expired weibo was not marked seen: %#v", fake.kv)
	}
	if len(fake.logs) == 0 || fake.logs[len(fake.logs)-1].Message != "微博过期博文已跳过" {
		t.Fatalf("expired weibo skip was not logged: %#v", fake.logs)
	}
}

func TestWeiboSubscriptionCheckFiltersByService(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{
		ID: "typed-weibo", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "group", TargetID: "10000", Services: []string{"image"}, Enabled: true,
	}
	fake.kv[weiboFeedSourceKey(item)] = true
	feed := weiboFeedResult(
		weiboTextMblog("text-1", "6000000001", "文字微博", now.Add(-time.Minute).Unix()),
		weiboImageMblog("image-1", "6000000001", "图片微博", now.Add(-time.Minute).Unix()),
		weiboVideoMblog("video-1", "6000000001", "视频微博", now.Add(-time.Minute).Unix()),
		weiboRepostMblog("repost-1", "6000000001", "转发微博", now.Add(-time.Minute).Unix()),
	)
	fake.httpResponses = []rayleabot.ActionResult{feed, avatarHTTPResult(), avatarHTTPResult()}
	result := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{Enabled: true, Subscriptions: []subscription{item}}, now)
	if intScalar(result["sent"]) != 1 {
		t.Fatalf("service filter result = %#v", result)
	}
	if fake.renders[0].Data["service"] != "图片" {
		t.Fatalf("filtered render = %#v", fake.renders[0].Data)
	}
	if _, exists := fake.kv["seen:typed-weibo:image:image-1"]; !exists {
		t.Fatalf("image update was not marked seen: %#v", fake.kv)
	}
	if _, exists := fake.kv["seen:typed-weibo:post:text-1"]; exists {
		t.Fatalf("unsubscribed post was marked seen")
	}
}

func TestWeiboSubscriptionCheckDelegatesMediaPrefetchToHost(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{
		ID: "retry-weibo-media", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "group", TargetID: "10000", Services: []string{"image"}, Enabled: true,
	}
	fake.kv[weiboFeedSourceKey(item)] = true
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/api/container/getIndex", result: weiboFeedResult(weiboImageMblog("retry-image", item.UID, "图片微博", now.Unix()))},
		{path: "/face.jpg", result: avatarHTTPResult()},
	}

	result := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{
		Enabled: true, Subscriptions: []subscription{item},
	}, now)

	if intScalar(result["sent"]) != 1 || boolScalar(result["degraded"]) {
		t.Fatalf("host-prefetched media should keep the plugin delivery healthy: %#v", result)
	}
	if len(fake.renders) != 1 || len(fake.messages) != 1 {
		t.Fatalf("media render was not delivered: renders=%d messages=%d", len(fake.renders), len(fake.messages))
	}
	media := mapSliceValue(fake.renders[0].Data["media_items"])
	if len(media) != 1 || stringScalar(media[0]["url"]) != "assets/grid.svg" || stringScalar(media[0]["resource_id"]) != "weibo-media-0" {
		t.Fatalf("media resource mapping = %#v", media)
	}
	if len(fake.resourceRenders) != 1 || len(fake.resourceRenders[0].Resources) != 1 || fake.resourceRenders[0].Resources[0].URL != "https://tvax2.sinaimg.cn/large/ok.jpg" {
		t.Fatalf("render resources = %#v", fake.resourceRenders)
	}
	if _, exists := fake.kv["seen:retry-weibo-media:image:retry-image"]; !exists {
		t.Fatalf("delivered placeholder card was not marked seen: %#v", fake.kv)
	}
}

func TestWeiboSubscriptionCheckPassesMultipleMediaResources(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now().Truncate(time.Second)
	item := subscription{
		ID: "retry-partial-weibo-media", Platform: "weibo", UID: "6000000001", Name: "测试博主",
		TargetType: "group", TargetID: "10000", Services: []string{"image"}, Enabled: true,
	}
	fake.kv[weiboFeedSourceKey(item)] = true
	mblog := weiboTextMblog("partial-image", item.UID, "多图微博", now.Unix())
	mblog["pics"] = []any{
		map[string]any{"url": "https://wx2.sinaimg.cn/orj360/good.jpg"},
		map[string]any{"url": "https://wx2.sinaimg.cn/orj360/broken.jpg"},
	}
	fake.httpRoutes = []fakeHTTPRoute{
		{path: "/api/container/getIndex", result: weiboFeedResult(mblog)},
		{path: "/face.jpg", result: avatarHTTPResult()},
	}

	result := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{
		Enabled: true, Subscriptions: []subscription{item},
	}, now)

	if intScalar(result["sent"]) != 1 || boolScalar(result["degraded"]) {
		t.Fatalf("host-prefetched media should keep the plugin delivery healthy: %#v", result)
	}
	if len(fake.renders) != 1 || len(fake.messages) != 1 {
		t.Fatalf("partial media failure did not deliver a card: renders=%d messages=%d", len(fake.renders), len(fake.messages))
	}
	media := mapSliceValue(fake.renders[0].Data["media_items"])
	if len(media) != 2 || stringScalar(media[0]["resource_id"]) != "weibo-media-0" || stringScalar(media[1]["resource_id"]) != "weibo-media-1" {
		t.Fatalf("media resource mapping = %#v", media)
	}
	if len(fake.resourceRenders) != 1 || len(fake.resourceRenders[0].Resources) != 2 || fake.resourceRenders[0].Resources[0].URL != "https://wx2.sinaimg.cn/large/good.jpg" || fake.resourceRenders[0].Resources[1].URL != "https://wx2.sinaimg.cn/large/broken.jpg" {
		t.Fatalf("render resources = %#v", fake.resourceRenders)
	}
	if _, exists := fake.kv["seen:retry-partial-weibo-media:image:partial-image"]; !exists {
		t.Fatalf("delivered card was not marked seen: %#v", fake.kv)
	}
}

func TestWeiboCheckDoesNotSkipWhenOnlyWeiboSubscriptions(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureWeiboAccounts("primary")
	now := time.Now()
	fake.httpResponses = []rayleabot.ActionResult{weiboFeedResult()}
	result := checkSubscriptionsWithActionsAt(context.Background(), fake, settings{Enabled: true, Subscriptions: []subscription{{
		ID: "weibo-only", Platform: "weibo", UID: "6000000001", Services: []string{"all"}, Enabled: true,
	}}}, now)
	if stringScalar(result["skipped"]) == "no_bilibili_subscriptions" || stringScalar(result["skipped"]) != "" {
		t.Fatalf("weibo-only check skipped = %#v", result)
	}
	if intScalar(result["checked"]) != 1 {
		t.Fatalf("weibo-only checked = %#v", result)
	}
	if _, exists := fake.kv["source:subscription:last_result"]; !exists {
		t.Fatalf("combined check result used a platform-specific key: %#v", fake.kv)
	}
}

func TestWeiboCheckSkipsWhenNothingCheckable(t *testing.T) {
	result := checkSubscriptionsWithActionsAt(context.Background(), newFakePluginActions(), settings{Enabled: true}, time.Now())
	if stringScalar(result["skipped"]) != "no_checkable_subscriptions" {
		t.Fatalf("empty check skipped = %#v", result)
	}
}

func TestWeiboPlainTextPreservesLineBreaksAndEscapedText(t *testing.T) {
	got := weiboPlainText(`第一行<br />第二行。。<p><a href="/n/test">链接</a>&lt;br&gt;字面</p>`)
	want := "第一行\n第二行。。\n链接<br>字面"
	if got != want {
		t.Fatalf("weiboPlainText() = %q, want %q", got, want)
	}
}

func TestWeiboNormalizationClassifiesServicesAndSkipsAds(t *testing.T) {
	updates := weiboFeedUpdates(map[string]any{"ok": 1, "data": map[string]any{"cards": []any{
		map[string]any{"card_type": 9, "mblog": weiboTextMblog("post-1", "6000000001", "文字微博", 1700000000)},
		map[string]any{"card_type": 9, "mblog": weiboImageMblog("image-1", "6000000001", "图片微博", 1700000000)},
		map[string]any{"card_type": 9, "mblog": weiboVideoMblog("video-1", "6000000001", "视频微博", 1700000000)},
		map[string]any{"card_type": 9, "mblog": weiboRepostMblog("repost-1", "6000000001", "转发微博", 1700000000)},
		map[string]any{"card_type": 9, "mblog": map[string]any{
			"mid": "ad-1", "is_ad": true, "created_timestamp": 1700000000,
			"user": map[string]any{"id": 6000000001, "screen_name": "广告"}, "text": "广告",
		}},
		map[string]any{"card_type": 9, "mblog": map[string]any{
			"mid": "promo-1", "promotion": map[string]any{"adtype": 1}, "created_timestamp": 1700000000,
			"user": map[string]any{"id": 6000000001, "screen_name": "推广"}, "text": "推广",
		}},
	}}})
	got := map[string]string{}
	for _, update := range updates {
		got[stringScalar(update["id"])] = stringScalar(update["service"])
	}
	want := map[string]string{"post-1": "post", "image-1": "image", "video-1": "video", "repost-1": "repost"}
	if len(got) != len(want) {
		t.Fatalf("updates = %#v", updates)
	}
	for id, service := range want {
		if got[id] != service {
			t.Fatalf("service[%s]=%q want %q in %#v", id, got[id], service, got)
		}
	}
}

func TestWeiboNormalizationPrefersLargeImageAndFormatsDecimalDuration(t *testing.T) {
	mblog := weiboVideoMblog("video-real", "6000000001", "视频微博", 1700000000)
	mblog["pics"] = []any{map[string]any{
		"url":   "https://wx2.sinaimg.cn/orj360/compact.jpg",
		"large": map[string]any{"url": "https://wx2.sinaimg.cn/mw2000/huge.jpg"},
	}}
	page := mapValue(mblog["page_info"])
	page["media_info"] = map[string]any{"duration": "279.498"}

	update := normalizeWeiboMblog(mblog, 0)
	images := imageMaps(update["images"], 9)
	if len(images) != 1 || stringScalar(images[0]["url"]) != "https://wx2.sinaimg.cn/mw2000/huge.jpg" {
		t.Fatalf("large image was not preferred: %#v", images)
	}
	if got := stringScalar(update["duration_text"]); got != "4:39" {
		t.Fatalf("decimal video duration = %q, want 4:39", got)
	}
}

func TestWeiboRenderDataKeepsAuthorSummaryImagesAndDropsUndeclaredHosts(t *testing.T) {
	update := normalizeWeiboMblog(weiboRepostMblog("repost-1", "6000000001", "转发微博", 1700000000), 0)
	if update == nil {
		t.Fatal("expected normalized repost")
	}
	images := imageMaps(update["images"], 9)
	if len(images) != 1 || stringScalar(images[0]["url"]) != "https://tvax2.sinaimg.cn/large/ok.jpg" {
		t.Fatalf("allowed image was lost: %#v", update["images"])
	}
	original := mapValue(update["original"])
	if original == nil || stringScalar(original["summary"]) != "原微博正文" {
		t.Fatalf("original was lost: %#v", original)
	}
	undeclared := normalizeWeiboMblog(map[string]any{
		"mid": "pic-bad", "created_timestamp": 1700000000, "text": "未声明图片",
		"user": map[string]any{"id": 6000000001, "screen_name": "测试博主"},
		"pics": []any{map[string]any{"url": "https://example.test/skip.jpg"}},
	}, 0)
	if stringScalar(undeclared["service"]) != "post" || len(imageMaps(undeclared["images"], 9)) != 0 {
		t.Fatalf("undeclared host should not be inlined: %#v", undeclared)
	}

	data := buildWeiboRenderData(subscription{
		Platform: "weibo", UID: "6000000001", Name: "测试博主",
		Subscribers: []subscriber{{ID: "10001", Nickname: "柒柒"}},
	}, update)
	if data["platform"] != "微博" || data["service"] != "转发" || !strings.Contains(stringScalar(data["summary"]), "转发微博") {
		t.Fatalf("render head = %#v", data)
	}
	author := mapValue(data["author"])
	if stringScalar(author["name"]) != "测试博主" || stringScalar(author["uid"]) != "6000000001" {
		t.Fatalf("author = %#v", author)
	}
	renderedOriginal := mapValue(data["original"])
	if renderedOriginal == nil || stringScalar(renderedOriginal["summary"]) != "原微博正文" {
		t.Fatalf("render original = %#v", renderedOriginal)
	}
	if stringScalar(data["subscriber_text"]) != "柒柒" {
		t.Fatalf("subscribers = %#v", data["subscriber_text"])
	}
}

func TestWeiboUpdateTemplatePreservesInlineAvatars(t *testing.T) {
	source, err := os.ReadFile("../../templates/weibo-update/template.html")
	if err != nil {
		t.Fatalf("read update template: %v", err)
	}
	compiled, err := template.New("weibo-update").Funcs(template.FuncMap{
		"safeHTML": func(value any) template.HTML { return template.HTML(stringScalar(value)) },
	}).Parse(string(source))
	if err != nil {
		t.Fatalf("parse update template: %v", err)
	}
	const avatar = "data:image/png;base64,fixture"
	data := map[string]any{
		"Stylesheet": template.CSS(""),
		"Theme":      "default",
		"service":    "转发", "category": "转发微博", "title": "测试微博",
		"author":   map[string]any{"name": "测试博主", "avatar": avatar},
		"original": map[string]any{"title": "原微博", "author": map[string]any{"name": "原作者", "avatar": avatar}},
		"subscriber_cards": []map[string]any{
			{"display_name": "柒柒", "avatar_url": avatar},
		},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute update template: %v", err)
	}
	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") {
		t.Fatalf("inline avatar was escaped: %s", html)
	}
	if count := strings.Count(html, `data-avatar="`+avatar+`"`); count != 3 {
		t.Fatalf("expected 3 inlined avatars, got %d: %s", count, html)
	}
	for _, marker := range []string{"image.src = source", `data-fallback="assets/weibo-default-avatar.svg"`, `src="assets/weibo-default-avatar.svg"`, "微博"} {
		if !strings.Contains(html, marker) {
			t.Fatalf("update template missing %q: %s", marker, html)
		}
	}
}

func TestWeiboUpdateTemplatePreservesInlineMediaAndDuration(t *testing.T) {
	source, err := os.ReadFile("../../templates/weibo-update/template.html")
	if err != nil {
		t.Fatalf("read update template: %v", err)
	}
	compiled, err := template.New("weibo-update").Funcs(template.FuncMap{
		"safeHTML": func(value any) template.HTML { return template.HTML(stringScalar(value)) },
	}).Parse(string(source))
	if err != nil {
		t.Fatalf("parse update template: %v", err)
	}
	const media = "data:image/jpeg;base64,fixture"
	data := map[string]any{
		"Stylesheet": template.CSS(""), "Theme": "default", "platform": "微博", "service": "视频",
		"subscription": map[string]any{},
		"media_items": []map[string]any{{
			"url": media, "fallback": "assets/cover.svg", "class": "media-item media-item--wide media-item--video", "duration_text": "4:39",
		}},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute update template: %v", err)
	}
	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") || !strings.Contains(html, `data-media="`+media+`"`) {
		t.Fatalf("inline media was escaped: %s", html)
	}
	for _, marker := range []string{`data-fallback="assets/cover.svg"`, `<span class="duration-badge">4:39</span>`, "image.dataset.avatar || image.dataset.media"} {
		if !strings.Contains(html, marker) {
			t.Fatalf("update template missing %q: %s", marker, html)
		}
	}
}

func TestWeiboUpdateTemplateOmitsMissingTitles(t *testing.T) {
	source, err := os.ReadFile("../../templates/weibo-update/template.html")
	if err != nil {
		t.Fatalf("read update template: %v", err)
	}
	compiled, err := template.New("weibo-update").Funcs(template.FuncMap{
		"safeHTML": func(value any) template.HTML { return template.HTML(stringScalar(value)) },
	}).Parse(string(source))
	if err != nil {
		t.Fatalf("parse update template: %v", err)
	}
	data := map[string]any{
		"Stylesheet":   template.CSS(""),
		"Theme":        "default",
		"platform":     "微博",
		"service":      "文字",
		"category":     "文字微博",
		"content_text": "正文只显示一次。",
		"author":       map[string]any{"name": "测试博主"},
		"original": map[string]any{
			"service": "图片", "category": "图片微博", "summary": "原微博正文。",
			"author": map[string]any{"name": "原作者"},
		},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute update template: %v", err)
	}
	html := output.String()
	if strings.Contains(html, "<h1>") || strings.Contains(html, "<h2>") {
		t.Fatalf("titleless Weibo rendered a heading: %s", html)
	}
	if strings.Count(html, "正文只显示一次。") != 1 || !strings.Contains(html, "原微博正文。") {
		t.Fatalf("titleless Weibo content was lost or duplicated: %s", html)
	}
}

func weiboFeedResult(mblogs ...map[string]any) rayleabot.ActionResult {
	return weiboFeedPageResult("", mblogs...)
}

func weiboFeedPageResult(cursor string, mblogs ...map[string]any) rayleabot.ActionResult {
	cards := make([]any, 0, len(mblogs))
	for _, mblog := range mblogs {
		cards = append(cards, map[string]any{"card_type": 9, "mblog": mblog})
	}
	data := map[string]any{"cards": cards}
	if cursor != "" {
		data["cardlistInfo"] = map[string]any{"since_id": cursor}
	}
	return httpJSONResult(200, map[string]any{"ok": 1, "data": data})
}

func weiboTextMblog(id, uid, text string, ts int64) map[string]any {
	return map[string]any{
		"mid": id, "idstr": id, "text": text, "created_timestamp": ts,
		"user": map[string]any{"id": uid, "screen_name": "测试博主", "avatar_hd": "https://tvax2.sinaimg.cn/face.jpg"},
	}
}

func weiboImageMblog(id, uid, text string, ts int64) map[string]any {
	mblog := weiboTextMblog(id, uid, text, ts)
	mblog["pics"] = []any{map[string]any{
		"url":   "https://example.test/skip.jpg",
		"large": map[string]any{"url": "https://tvax2.sinaimg.cn/large/ok.jpg", "geo": map[string]any{"width": 800, "height": 600}},
	}}
	return mblog
}

func weiboVideoMblog(id, uid, text string, ts int64) map[string]any {
	mblog := weiboTextMblog(id, uid, text, ts)
	mblog["page_info"] = map[string]any{
		"type": "video", "page_title": text,
		"page_pic":   map[string]any{"url": "https://wx1.sinaimg.cn/large/cover.jpg"},
		"media_info": map[string]any{"duration": 80, "duration_player": "1:20"},
	}
	return mblog
}

func weiboRepostMblog(id, uid, text string, ts int64) map[string]any {
	mblog := weiboImageMblog(id, uid, text, ts)
	mblog["retweeted_status"] = map[string]any{
		"mid": id + "-orig", "idstr": id + "-orig", "text": "原微博正文", "created_timestamp": ts - 60,
		"user": map[string]any{"id": "6000000008", "screen_name": "原作者"},
		"pics": []any{map[string]any{"large": map[string]any{"url": "https://wx2.sinaimg.cn/large/orig.jpg"}}},
	}
	return mblog
}

func countHTTPByPath(fake *fakePluginActions, path string) int {
	count := 0
	for _, request := range fake.httpRequests {
		parsed, err := url.Parse(request.URL)
		if err == nil && parsed.Path == path {
			count++
		}
	}
	return count
}

func requestURLs(fake *fakePluginActions) []string {
	urls := make([]string, 0, len(fake.httpRequests))
	for _, request := range fake.httpRequests {
		urls = append(urls, request.Method+" "+request.URL)
	}
	return urls
}
