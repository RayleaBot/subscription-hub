package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

var workflowNow = time.Unix(1_700_000_000, 0)

type workflowSession struct {
	result     PollResult
	prepare    func(Update) (Update, error)
	poll       func()
	resolve    Resolution
	resolveErr error
}

func (session *workflowSession) Resolve(context.Context, ResolveRequest) (Resolution, error) {
	return session.resolve, session.resolveErr
}

func (session *workflowSession) Poll(context.Context, context.Context, []Subscription, time.Time) PollResult {
	if session.poll != nil {
		session.poll()
	}
	return session.result
}

func (session *workflowSession) Prepare(ctx context.Context, update Update) (Update, error) {
	if session.prepare != nil {
		return session.prepare(update)
	}
	return CloneJSONMap(update), nil
}

func (session *workflowSession) UpdateCard(item Subscription, update Update) CardRequest {
	return CardRequest{Template: "fixture-update", Data: update, Fallback: "fixture"}
}

func workflowPlatform(id string, session *workflowSession) Platform {
	return Platform{
		ID: id, Name: id, SubjectLabel: "ID", ParseSubject: SafeSubjectID,
		Services:   NewServiceCatalog([]string{"video", "live"}, map[string]string{"all": "all", "video": "video", "live": "live"}, nil),
		NewSession: func(SourceActions) Session { return session },
		Baseline:   &BaselinePolicy{KeyPrefix: "source:" + id + ":feed:initialized:", Timestamp: true, ExemptServices: []string{"live"}},
	}
}

func newWorkflowHandler(t testing.TB, platforms ...Platform) *Handler {
	t.Helper()
	if len(platforms) == 0 {
		platforms = []Platform{workflowPlatform("fixture", &workflowSession{})}
	}
	handler, err := NewHandler(Options{MediaTempRoot: t.TempDir(), Platforms: platforms, Now: func() time.Time { return workflowNow }, Jitter: func() time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func workflowSubscription(id, platform string) Subscription {
	return Subscription{ID: id, Platform: platform, UID: "subject", TargetType: "private", TargetID: id, Services: []string{"all"}, Enabled: true}
}

func workflowUpdate(platform, id string) Update {
	return Update{"id": id, "uid": "subject", "platform": platform, "service": "video", "pub_ts": workflowNow.Unix()}
}

func readySession(platform string) *workflowSession {
	return &workflowSession{result: PollResult{
		Checked: 1, Updates: []Update{workflowUpdate(platform, "new")},
		ReadyUIDs: map[string]bool{"subject": true}, Summary: map[string]any{"accounts": 1, "feed_ok": true},
	}}
}

func initializeWorkflow(actions *testkit.Actions, subscriptions []Subscription) {
	for _, item := range subscriptions {
		actions.KV["source:"+item.Platform+":feed:initialized:"+item.ID] = true
	}
}

func TestCheckIsolatesTargetsAndSourcesAndRetriesOnlyFailedDelivery(t *testing.T) {
	actions := testkit.NewActions()
	current := Settings{Enabled: true, Subscriptions: []Subscription{
		workflowSubscription("first", "alpha"), workflowSubscription("second", "alpha"), workflowSubscription("third", "beta"),
	}}
	initializeWorkflow(actions, current.Subscriptions)
	alpha, beta := readySession("alpha"), readySession("beta")
	alpha.result.Updates = append(alpha.result.Updates, workflowUpdate("alpha", "legacy"))
	for _, item := range current.Subscriptions[:2] {
		actions.KV[SubscriptionUpdateKey(item, alpha.result.Updates[1])] = true
	}
	actions.MessageErrors = []error{nil, errors.New("target unavailable"), nil}
	handler := newWorkflowHandler(t, workflowPlatform("alpha", alpha), workflowPlatform("beta", beta))
	first := handler.Check(t.Context(), actions, current)
	if IntScalar(first["checked"]) != 2 || IntScalar(first["sent"]) != 2 || !BoolScalar(first["degraded"]) {
		t.Fatalf("first check = %#v", first)
	}
	if _, exists := actions.KV["seen:second:video:new"]; exists {
		t.Fatal("failed target was marked delivered")
	}
	if actions.KV["seen:first:video:new"] == nil || actions.KV["seen:third:video:new"] == nil {
		t.Fatal("successful delivery state missing")
	}
	second := handler.Check(t.Context(), actions, current)
	if IntScalar(second["sent"]) != 1 || BoolScalar(second["degraded"]) || len(actions.Messages) != 4 || actions.Messages[3].TargetID != "second" {
		t.Fatalf("retry duplicated a successful delivery: result=%#v messages=%#v", second, actions.Messages)
	}
}

func TestCheckPreparationAndRenderingFailuresPreserveDeliveryState(t *testing.T) {
	for _, test := range []struct {
		name, stage         string
		recoverable, render bool
	}{
		{name: "recoverable content", stage: "prepare", recoverable: true},
		{name: "unusable content", stage: "prepare"},
		{name: "render failure", stage: "render", render: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := testkit.NewActions()
			current := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha"), workflowSubscription("two", "beta")}}
			initializeWorkflow(actions, current.Subscriptions)
			alpha, beta := readySession("alpha"), readySession("beta")
			if test.render {
				actions.RenderErrors = []error{errors.New("render unavailable")}
			} else {
				alpha.prepare = func(update Update) (Update, error) {
					if test.recoverable {
						return update, errors.New("content incomplete")
					}
					return nil, errors.New("content unavailable")
				}
			}
			handler := newWorkflowHandler(t, workflowPlatform("alpha", alpha), workflowPlatform("beta", beta))
			result := handler.Check(t.Context(), actions, current)
			want := int64(1)
			if test.recoverable {
				want = 2
			}
			if IntScalar(result["sent"]) != want || !BoolScalar(result["degraded"]) {
				t.Fatalf("check = %#v", result)
			}
			if !strings.Contains(strings.Join(stringSlice(result["errors"]), "\n"), "[alpha/"+test.stage+"]") {
				t.Fatalf("failure has no source/stage: %#v", result)
			}
			_, seen := actions.KV["seen:one:video:new"]
			if seen != test.recoverable || actions.KV["seen:two:video:new"] == nil {
				t.Fatalf("incorrect delivery markers: %#v", actions.KV)
			}
		})
	}
}

func TestCheckCommitsOnlyReadyBaselinesAndSkipsHistoricalContent(t *testing.T) {
	actions := testkit.NewActions()
	current := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha"), workflowSubscription("two", "beta")}}
	alpha, beta := readySession("alpha"), readySession("beta")
	beta.result.ReadyUIDs["subject"] = false
	handler := newWorkflowHandler(t, workflowPlatform("alpha", alpha), workflowPlatform("beta", beta))
	first := handler.Check(t.Context(), actions, current)
	if IntScalar(first["sent"]) != 0 || actions.KV["source:alpha:feed:initialized:one"] == nil || actions.KV["source:beta:feed:initialized:two"] != nil {
		t.Fatalf("unexpected first baseline: result=%#v kv=%#v", first, actions.KV)
	}
	old := workflowUpdate("alpha", "old-unseen")
	newer := workflowUpdate("alpha", "newer")
	newer["pub_ts"] = workflowNow.Add(time.Second).Unix()
	alpha.result.Updates = []Update{old, newer}
	beta.result.ReadyUIDs["subject"] = true
	second := handler.Check(t.Context(), actions, current)
	if IntScalar(second["sent"]) != 1 || actions.KV["seen:one:video:old-unseen"] == nil || actions.KV["source:beta:feed:initialized:two"] == nil {
		t.Fatalf("baseline filtering changed: result=%#v kv=%#v", second, actions.KV)
	}
}

func TestCheckDefersBooleanBaselineUntilHistoryIsFullyRecorded(t *testing.T) {
	for _, interrupted := range []bool{true, false} {
		name := "seen write failure"
		if interrupted {
			name = "work deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				actions := &stateFailureActions{Actions: testkit.NewActions()}
				item := workflowSubscription("one", "bilibili")
				current := Settings{Enabled: true, Subscriptions: []Subscription{item}}
				session := readySession("bilibili")
				session.result.Updates = []Update{workflowUpdate("bilibili", "history-one"), workflowUpdate("bilibili", "history-two")}
				firstKey := SubscriptionUpdateKey(item, session.result.Updates[0])
				secondKey := SubscriptionUpdateKey(item, session.result.Updates[1])
				if interrupted {
					actions.afterSet = func(key string) {
						if key == firstKey {
							time.Sleep(SubscriptionCheckTimeout)
							synctest.Wait()
						}
					}
				} else {
					actions.failSet = secondKey
				}
				platform := workflowPlatform("bilibili", session)
				platform.Baseline.KeyPrefix = "source:bilibili:dynamic:initialized:"
				platform.Baseline.Timestamp = false
				baselineKey := platform.Baseline.KeyPrefix + item.ID
				handler := newWorkflowHandler(t, platform)
				first := handler.Check(t.Context(), actions, current)
				if IntScalar(first["sent"]) != 0 || !BoolScalar(first["degraded"]) || actions.KV[firstKey] == nil || actions.KV[secondKey] != nil {
					t.Fatalf("expected partially recorded history: result=%#v kv=%#v", first, actions.KV)
				}
				if actions.KV[baselineKey] != nil {
					t.Errorf("incomplete history committed a boolean baseline: %#v", actions.KV)
				}
				actions.afterSet, actions.failSet = nil, ""
				second := handler.Check(t.Context(), actions, current)
				if IntScalar(second["sent"]) != 0 || BoolScalar(second["degraded"]) || len(actions.Messages) != 0 || actions.KV[secondKey] == nil || actions.KV[baselineKey] != true {
					t.Errorf("retry must finish the baseline without historical delivery: result=%#v kv=%#v", second, actions.KV)
				}
				session.result.Updates = append(session.result.Updates, workflowUpdate("bilibili", "after-baseline"))
				third := handler.Check(t.Context(), actions, current)
				if IntScalar(third["sent"]) != 1 || BoolScalar(third["degraded"]) || actions.KV["seen:one:video:after-baseline"] == nil {
					t.Fatalf("new content after the baseline was not delivered: %#v", third)
				}
			})
		})
	}
}

func TestCheckKeepsTimestampedBaselineAfterInterruptedHistory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		actions := &stateFailureActions{Actions: testkit.NewActions()}
		item := workflowSubscription("one", "alpha")
		current := Settings{Enabled: true, Subscriptions: []Subscription{item}}
		session := readySession("alpha")
		session.result.Updates = []Update{workflowUpdate("alpha", "history-one"), workflowUpdate("alpha", "history-two")}
		firstKey := SubscriptionUpdateKey(item, session.result.Updates[0])
		secondKey := SubscriptionUpdateKey(item, session.result.Updates[1])
		actions.afterSet = func(key string) {
			if key == firstKey {
				time.Sleep(SubscriptionCheckTimeout)
				synctest.Wait()
			}
		}
		platform := workflowPlatform("alpha", session)
		handler := newWorkflowHandler(t, platform)
		first := handler.Check(t.Context(), actions, current)
		initialized, baselineAt, err := ReadBaseline(t.Context(), actions, platform.Baseline.KeyPrefix+item.ID)
		if err != nil || !initialized || !baselineAt.Equal(workflowNow) || IntScalar(first["sent"]) != 0 || !BoolScalar(first["degraded"]) || actions.KV[secondKey] != nil {
			t.Fatalf("timestamped baseline did not survive interrupted history: result=%#v kv=%#v err=%v", first, actions.KV, err)
		}
		actions.afterSet = nil
		newer := workflowUpdate("alpha", "after-baseline")
		newer["pub_ts"] = workflowNow.Add(time.Second).Unix()
		session.result.Updates = append(session.result.Updates, newer)
		second := handler.Check(t.Context(), actions, current)
		if IntScalar(second["sent"]) != 1 || BoolScalar(second["degraded"]) || len(actions.Messages) != 1 || actions.KV[secondKey] == nil || actions.KV[SubscriptionUpdateKey(item, newer)] == nil {
			t.Fatalf("timestamped retry did not separate historical and new content: result=%#v kv=%#v", second, actions.KV)
		}
	})
}

type cancelDeliveryActions struct {
	*testkit.Actions
	cancel context.CancelFunc
}

func (actions *cancelDeliveryActions) MessageSend(ctx context.Context, request rayleabot.MessageSendRequest) (rayleabot.ActionResult, error) {
	result, err := actions.Actions.MessageSend(ctx, request)
	actions.cancel()
	return result, err
}

func TestCheckFinalizesSuccessfulSendAfterWorkDeadlineAndStopsNewWork(t *testing.T) {
	checkCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	actions := &cancelDeliveryActions{Actions: testkit.NewActions(), cancel: cancel}
	current := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha"), workflowSubscription("two", "alpha"), workflowSubscription("three", "beta")}}
	initializeWorkflow(actions.Actions, current.Subscriptions)
	alpha, beta := readySession("alpha"), readySession("beta")
	beta.poll = func() { t.Fatal("started another source after the work deadline") }
	handler := newWorkflowHandler(t, workflowPlatform("alpha", alpha), workflowPlatform("beta", beta))
	result := handler.check(checkCtx, t.Context(), actions, current, workflowNow)
	if IntScalar(result["sent"]) != 1 || !BoolScalar(result["degraded"]) || len(actions.Messages) != 1 || len(actions.Renders) != 1 {
		t.Fatalf("check exceeded its work context: %#v", result)
	}
	if actions.KV["seen:one:video:new"] == nil || actions.KV["source:subscription:last_result"] == nil {
		t.Fatal("reserved context did not finalize state")
	}
}

func TestCheckFailureRedactsCredentialsAcrossSources(t *testing.T) {
	actions := testkit.NewActions()
	session := readySession("alpha")
	session.result.Errors = []string{"SESSDATA=fixture-bili; SUB=fixture-weibo; msToken=fixture-token; ttwid=fixture-device; Authorization=Bearer fixture-auth"}
	handler := newWorkflowHandler(t, workflowPlatform("alpha", session))
	result := handler.Check(t.Context(), actions, Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha")}})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-bili", "fixture-weibo", "fixture-token", "fixture-device", "fixture-auth"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("check failure leaked %q", secret)
		}
	}
	if !strings.Contains(string(encoded), "[alpha/source]") {
		t.Fatalf("missing failure location: %s", encoded)
	}
}

func TestPlatformAssemblyRejectsAmbiguousRoutes(t *testing.T) {
	for _, test := range []struct {
		name      string
		platforms []Platform
	}{
		{name: "duplicate platform", platforms: []Platform{workflowPlatform("alpha", &workflowSession{}), workflowPlatform("alpha", &workflowSession{})}},
		{name: "missing source factory", platforms: []Platform{{ID: "alpha", Name: "alpha"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewHandler(Options{Platforms: test.platforms}); err == nil {
				t.Fatal("ambiguous assembly accepted")
			}
		})
	}
	platform := workflowPlatform("alpha", &workflowSession{})
	platform.Commands = map[string]string{"订阅列表": "add"}
	if _, err := NewHandler(Options{Platforms: []Platform{platform}}); err == nil {
		t.Fatal("platform overrode a shared command")
	}
}

func TestSourceBoundaryStopsCanceledRequestsAndDoesNotExposeDelivery(t *testing.T) {
	actions := testkit.NewActions()
	source := sourceBoundary(actions)
	if _, ok := source.(HostActions); ok {
		t.Fatal("source received render and delivery actions")
	}
	if err := source.(GenericLocalActionCaller).Call(t.Context(), "render.image", map[string]any{}, &rayleabot.ActionResult{}); err == nil {
		t.Fatal("source could render through the generic action caller")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := source.HTTPRequest(ctx, rayleabot.HTTPRequest{URL: "https://example.test/"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled source request: %v", err)
	}
	if len(actions.HTTPRequests) != 0 || len(actions.Renders) != 0 {
		t.Fatal("blocked source action reached the host")
	}
}

type stateFailureActions struct {
	*testkit.Actions
	failGet, failSet string
	afterSet         func(string)
}

func (actions *stateFailureActions) KVGet(ctx context.Context, key string) (rayleabot.ActionResult, error) {
	if key == actions.failGet {
		return nil, errors.New("fixture storage unavailable")
	}
	return actions.Actions.KVGet(ctx, key)
}

func (actions *stateFailureActions) KVSet(ctx context.Context, key string, value any) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == actions.failSet {
		return nil, errors.New("fixture storage unavailable")
	}
	result, err := actions.Actions.KVSet(ctx, key, value)
	if err == nil && actions.afterSet != nil {
		actions.afterSet(key)
	}
	return result, err
}

func TestCheckKeepsLiveDeliveryIndependentOfFeedBaseline(t *testing.T) {
	for _, services := range [][]string{{"live"}, {"all"}} {
		t.Run(services[0], func(t *testing.T) {
			actions := &stateFailureActions{Actions: testkit.NewActions(), failGet: "source:alpha:feed:initialized:one"}
			session := readySession("alpha")
			live := workflowUpdate("alpha", "live-session")
			live["service"] = "live"
			session.result.Updates = append(session.result.Updates, live)
			item := workflowSubscription("one", "alpha")
			item.Services = services
			handler := newWorkflowHandler(t, workflowPlatform("alpha", session))
			result := handler.Check(t.Context(), actions, Settings{Enabled: true, Subscriptions: []Subscription{item}})
			if IntScalar(result["sent"]) != 1 || actions.KV["seen:one:live:live-session"] == nil || actions.KV["seen:one:video:new"] != nil {
				t.Fatalf("feed state interfered with live delivery: %#v", result)
			}
			if BoolScalar(result["degraded"]) != (services[0] == "all") {
				t.Fatalf("unnecessary baseline access: %#v", result)
			}
		})
	}
}

func TestCheckReportsSummaryPersistenceFailure(t *testing.T) {
	actions := &stateFailureActions{Actions: testkit.NewActions(), failSet: "source:subscription:last_result"}
	current := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha")}}
	initializeWorkflow(actions.Actions, current.Subscriptions)
	handler := newWorkflowHandler(t, workflowPlatform("alpha", readySession("alpha")))
	result := handler.Check(t.Context(), actions, current)
	if IntScalar(result["sent"]) != 1 || !BoolScalar(result["degraded"]) || actions.KV["seen:one:video:new"] == nil {
		t.Fatalf("summary persistence hid successful delivery or its failure: %#v", result)
	}
	if !strings.Contains(strings.Join(stringSlice(result["errors"]), "\n"), "[subscription/state]") {
		t.Fatalf("missing state failure: %#v", result)
	}
}

func TestSubscriptionCheckLogKeepsCompleteFailuresAndReadableSummary(t *testing.T) {
	actions := testkit.NewActions()
	failures := []string{
		"[douyin/source] 抖音检查因平台风控暂停，剩余约 1 分钟。",
		"[weibo/source] 微博检查触发频率限制。",
		"[bilibili/state] Bilibili 状态保存失败。",
		"[subscription/timeout] 订阅检查未在本轮完成。",
	}
	logSubscriptionCheck(t.Context(), actions, map[string]any{"checked": 12, "sent": 2, "errors": failures})
	if len(actions.Logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(actions.Logs))
	}
	entry := actions.Logs[0]
	if entry.Level != "warn" || IntScalar(entry.Fields["checked"]) != 12 || IntScalar(entry.Fields["sent"]) != 2 {
		t.Fatalf("wrong check result log: %#v", entry)
	}
	if IntScalar(entry.Fields["failure_count"]) != int64(len(failures)) || len(stringSlice(entry.Fields["errors"])) != len(failures) {
		t.Fatalf("structured failures were truncated: %#v", entry.Fields)
	}
}

func TestRemovedSubscriptionKeepsOtherPlatformState(t *testing.T) {
	actions := testkit.NewActions()
	removed := workflowSubscription("one", "alpha")
	alpha, beta := workflowPlatform("alpha", &workflowSession{}), workflowPlatform("beta", &workflowSession{})
	alpha.Cleanup = func(item Subscription) []KVSelector {
		return []KVSelector{{Prefix: "source:alpha:subject:" + item.UID}}
	}
	actions.KV["seen:one:video:new"] = true
	actions.KV["source:alpha:subject:subject"] = true
	actions.KV["source:beta:subject:subject"] = true
	actions.KV["seen:two:video:new"] = true
	handler := newWorkflowHandler(t, alpha, beta)
	handler.cleanupRemovedSubscriptionKV(t.Context(), actions, &Settings{Subscriptions: []Subscription{workflowSubscription("two", "beta")}}, &removed, []string{"all"})
	if actions.KV["seen:one:video:new"] != nil || actions.KV["source:alpha:subject:subject"] != nil {
		t.Fatalf("removed source state remains: %#v", actions.KV)
	}
	if actions.KV["seen:two:video:new"] == nil || actions.KV["source:beta:subject:subject"] == nil {
		t.Fatalf("another platform lost state: %#v", actions.KV)
	}
}
