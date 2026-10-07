package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

type mediaWorkflowSession struct {
	*workflowSession
	plan ResolverMediaPlan
	err  error
}

func (session *mediaWorkflowSession) ResolverMedia(context.Context, Update, ResolverMediaSettings) (ResolverMediaPlan, error) {
	return session.plan, session.err
}

func mediaWorkflowPlatform(session *mediaWorkflowSession) Platform {
	platform := workflowPlatform("fixture", session.workflowSession)
	platform.NewSession = func(SourceActions) Session { return session }
	return platform
}

func waitMediaJobs(t *testing.T, handler *Handler, count int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for len(handler.deferredMedia.readyJobs()) != count {
		select {
		case <-deadline.C:
			t.Fatal("media preparation did not complete")
		case <-tick.C:
		}
	}
}

func TestSubscriptionForwardsMediaWithEachTargetsSubscriberAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("original")) }))
	defer server.Close()
	session := &mediaWorkflowSession{workflowSession: readySession("fixture"), plan: ResolverMediaPlan{Sources: []ResolverMediaSource{
		{Kind: "image", URLs: []string{server.URL}, FileName: "original.jpg"},
		{Kind: "video", URLs: []string{server.URL}, FileName: "original.mp4"},
	}}}
	session.result.Updates[0]["author"] = map[string]any{"name": "作者的新昵称"}
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, mediaWorkflowPlatform(session))
	handler.actions = actions
	first, second := workflowSubscription("10001", "fixture"), workflowSubscription("10002", "fixture")
	first.TargetType, second.TargetType = "group", "group"
	first.Subscribers = []Subscriber{{ID: "20001", Nickname: "旧昵称"}}
	second.Subscribers = []Subscriber{{ID: "20002", Nickname: "另一群订阅人"}}
	actions.GroupMembers[GroupMemberKey(first.TargetID, "20001")] = rayleabot.ActionResult{"user_id": "20001", "nickname": "新昵称", "card": "实时群名片", "role": "member"}
	actions.GroupMembers[GroupMemberKey(second.TargetID, "20002")] = rayleabot.ActionResult{"user_id": "20002", "nickname": "另一群订阅人", "role": "member"}
	settings := Settings{Enabled: true, Subscriptions: []Subscription{first, second}}
	initializeWorkflow(actions, settings.Subscriptions)
	result := handler.Check(t.Context(), actions, settings)
	if result["sent"] != 2 || BoolScalar(result["degraded"]) || len(actions.Messages) != 2 {
		t.Fatalf("check = %#v, cards=%d", result, len(actions.Messages))
	}
	waitMediaJobs(t, handler, 2)
	// 配置和当前调度事件的操作者都不能替换已排队任务的发送人。
	settings.Subscriptions[0].Subscribers[0].Nickname = "后续修改"
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{Event: rayleabot.Event{Actor: rayleabot.Actor{ID: "99999", Nickname: "检查管理员"}}})
	if len(actions.ForwardRequests) != 2 {
		t.Fatalf("forwards = %#v", actions.ForwardRequests)
	}
	for _, request := range actions.ForwardRequests {
		wantID, wantName := "20001", "实时群名片"
		if request["target_id"] == "10002" {
			wantID, wantName = "20002", "另一群订阅人"
		}
		if request["source"] != "作者的新昵称" || len(MapSliceValue(request["messages"])) != 2 || forwardMedia(request, 0)["type"] != "image" || forwardMedia(request, 1)["type"] != "video" {
			t.Fatalf("subscription media = %#v", request)
		}
		for index := range 2 {
			if node := forwardNode(request, index); node["uin"] != wantID || node["name"] != wantName {
				t.Fatalf("sender = %#v", node)
			}
		}
	}
	if result := handler.Check(t.Context(), actions, settings); result["sent"] != 0 || len(actions.Messages) != 2 || len(handler.deferredMedia.jobs) != 0 {
		t.Fatalf("subscription replayed: %#v", result)
	}
}

func TestSubscriptionMediaFailuresKeepConfirmedCardDeduplication(t *testing.T) {
	for _, reason := range []string{"planning", "queue_full", "duration", "card_failed"} {
		t.Run(reason, func(t *testing.T) {
			session := &mediaWorkflowSession{workflowSession: readySession("fixture"), plan: ResolverMediaPlan{Sources: []ResolverMediaSource{{Kind: "video", URLs: []string{"https://unused.invalid/video"}}}}}
			actions := testkit.NewActions()
			handler := newWorkflowHandler(t, mediaWorkflowPlatform(session))
			settings := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("10001", "fixture")}}
			initializeWorkflow(actions, settings.Subscriptions)
			switch reason {
			case "planning":
				session.err = errors.New("fixture media failure")
			case "duration":
				session.err = &ResolverMediaSkippedError{Reason: "fixture limit"}
			case "card_failed":
				actions.MessageErrors = []error{errors.New("fixture card rejected")}
			case "queue_full":
				for range deferredMediaMaxJobs {
					handler.deferredMedia.push(&deferredMediaJob{})
				}
			}
			result := handler.Check(t.Context(), actions, settings)
			if reason == "card_failed" {
				if result["sent"] != 0 || len(handler.deferredMedia.jobs) != 0 {
					t.Fatalf("failed card queued media: %#v", result)
				}
				return
			}
			if result["sent"] != 1 || BoolScalar(result["degraded"]) != (reason != "duration") {
				t.Fatalf("check = %#v", result)
			}
			if result := handler.Check(t.Context(), actions, settings); result["sent"] != 0 || len(actions.Messages) != 1 {
				t.Fatalf("confirmed card replayed: %#v", result)
			}
		})
	}
}

func TestPartialDeferredForwardIsNotRetried(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	actions.ForwardErrors = []error{nil, &rayleabot.ActionError{Code: "platform.rate_limited"}}
	handler.actions = actions
	handler.deferredMedia.push(&deferredMediaJob{TargetType: "private", TargetID: "10001", Platform: "douyin", Settings: ResolverMediaSettings{ImageBatchSize: 1}, prepared: []preparedResolverMedia{{Kind: "image", Path: "one"}, {Kind: "video", Path: "two"}}})
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	if len(actions.ForwardRequests) != 2 || len(handler.deferredMedia.jobs) != 0 {
		t.Fatalf("partial batch replayed: %#v", actions.ForwardRequests)
	}
}
