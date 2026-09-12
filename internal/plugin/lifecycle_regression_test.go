package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

type delayedMediaActions struct {
	*testkit.Actions
	received chan rayleabot.MessageSendRequest
	confirm  <-chan struct{}
}

func (actions *delayedMediaActions) MessageSend(ctx context.Context, request rayleabot.MessageSendRequest) (rayleabot.ActionResult, error) {
	actions.received <- request
	select {
	case <-actions.confirm:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	path, _ := request.Message.Segments[0].Data["file"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if string(data) != "fixture" {
		return nil, fmt.Errorf("unexpected media content")
	}
	return actions.Actions.MessageSend(ctx, request)
}

func TestMediaIsReleasedOnlyAfterDelayedAdapterConfirmation(t *testing.T) {
	handler := newWorkflowHandler(t)
	confirm := make(chan struct{})
	actions := &delayedMediaActions{Actions: testkit.NewActions(), received: make(chan rayleabot.MessageSendRequest, 2), confirm: confirm}
	handler.actions = actions
	root, err := handler.deferredMedia.createTemp()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "fixture.mp4")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := &deferredMediaJob{TargetType: "group", TargetID: "fixture-target", Platform: "douyin", tempRoot: root, prepared: []preparedResolverMedia{{Kind: "video", Path: file}}}
	if !handler.deferredMedia.push(job) {
		t.Fatal("queue full")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.flushDeferredMedia(ctx, &rayleabot.EventContext{})
	}()
	select {
	case request := <-actions.received:
		if request.Message.Segments[0].Data["file"] != file {
			t.Error("adapter did not receive the original media path")
		}
	case <-ctx.Done():
		t.Fatal("adapter did not receive media")
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("media removed while adapter was waiting to read it")
	}
	close(confirm)
	<-done
	if len(actions.Messages) != 1 {
		t.Fatalf("confirmed sends = %d, want 1", len(actions.Messages))
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("confirmed media was not released")
	}
}

func TestExplicitRateLimitRetainsMediaForOnlyTheNextAttempt(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	actions.MessageErrors = []error{&rayleabot.ActionError{Code: "platform.rate_limited", Message: "fixture explicit refusal"}, nil}
	handler.actions = actions
	root, err := handler.deferredMedia.createTemp()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "fixture.mp4")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !handler.deferredMedia.push(&deferredMediaJob{TargetType: "group", TargetID: "fixture-target", Platform: "douyin", tempRoot: root, prepared: []preparedResolverMedia{{Kind: "video", Path: file}}}) {
		t.Fatal("queue full")
	}
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	if _, err := os.Stat(file); err != nil {
		t.Fatal("explicitly rejected send lost its retry file")
	}
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	if len(actions.Messages) != 2 {
		t.Fatalf("attempts = %d, want refusal then success", len(actions.Messages))
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("successful retry retained media")
	}
}

func TestSchedulerRegistrationIsSingleFlight(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.actions = actions
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			if !handler.ensureScheduler(t.Context(), &rayleabot.EventContext{}) {
				t.Error("registration failed")
			}
		})
	}
	workers.Wait()
	if len(actions.SchedulerRequests) != 3 {
		t.Fatalf("registrations = %d", len(actions.SchedulerRequests))
	}
}

func TestManualCheckCanCancelWhileAnotherCheckRuns(t *testing.T) {
	handler := newWorkflowHandler(t)
	handler.checkGate <- struct{}{}
	defer func() { <-handler.checkGate }()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := handler.Check(ctx, testkit.NewActions(), Settings{})
	if result["skipped"] != "check_canceled" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCheckBudgetIncludesTimeAlreadySpent(t *testing.T) {
	deadline := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	work, state, release := newSubscriptionCheckContexts(ctx)
	defer release()
	workDeadline, _ := work.Deadline()
	stateDeadline, _ := state.Deadline()
	if stateDeadline.After(deadline) || workDeadline.After(deadline.Add(-SubscriptionCheckFinalizationReserve)) {
		t.Fatal("check budget exceeded parent or consumed finalization reserve")
	}
}

func TestCooldownRemainsVisibleWithoutRepeatedWarningOrFalseRecovery(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.logCheck(t.Context(), actions, map[string]any{"errors": []string{"fixture upstream refusal"}})
	handler.logCheck(t.Context(), actions, map[string]any{"paused": true, "paused_only": true, "errors": []string{"fixture cooldown"}})
	if len(actions.Logs) != 2 || actions.Logs[0].Level != "warn" || actions.Logs[1].Level != "debug" {
		t.Fatalf("cooldown levels = %#v", actions.Logs)
	}
	handler.logCheck(t.Context(), actions, map[string]any{"errors": []string{}, "checked": 1})
	if len(actions.Logs) != 3 || actions.Logs[2].Level != "info" || IntScalar(actions.Logs[2].Fields["checked"]) != 1 {
		t.Fatal("actual recovery not recorded")
	}
}

func TestCheckWarningsAggregateByStructuredCauseAndKeepNewFailuresVisible(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }
	logFailure := func(cause, message string) {
		handler.logCheck(t.Context(), actions, map[string]any{"errors": []string{message}, "failure_causes": []string{cause}})
	}
	logFailure("douyin:source:session_blocked", "fixture refusal 1")
	now = now.Add(time.Minute)
	logFailure("douyin:source:session_blocked", "fixture different text and countdown")
	if len(actions.Logs) != 1 {
		t.Fatal("dynamic text bypassed structured aggregation")
	}
	logFailure("weibo:source:rate_limit", "fixture other platform failure")
	if len(actions.Logs) != 2 {
		t.Fatal("new failure was hidden by another source's throttle")
	}
	now = now.Add(5 * time.Minute)
	logFailure("douyin:source:session_blocked", "fixture continued refusal")
	if len(actions.Logs) != 3 || IntScalar(actions.Logs[2].Fields["repeat_count"]) != 2 {
		t.Fatal("summary did not represent suppressed and current occurrences exactly once")
	}
	handler.logCheck(t.Context(), actions, map[string]any{"errors": []string{}, "checked": 1})
	if len(actions.Logs) != 4 || IntScalar(actions.Logs[3].Fields["repeat_count"]) != 4 {
		t.Fatal("recovery lost occurrence counts")
	}
}

func TestUnconfirmedMediaKeepsFileAndDoesNotResend(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	actions.MessageErrors = []error{&rayleabot.ActionError{Code: "adapter.send_unconfirmed", Message: "fixture receipt timeout"}}
	handler.actions = actions
	root, err := handler.deferredMedia.createTemp()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "fixture.mp4")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := &deferredMediaJob{TargetType: "group", TargetID: "fixture-target", Platform: "douyin", tempRoot: root, prepared: []preparedResolverMedia{{Kind: "video", Path: file}}}
	if !handler.deferredMedia.push(job) {
		t.Fatal("queue full")
	}
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	if len(actions.Messages) != 1 {
		t.Fatalf("uncertain send replayed or sent a second notice: %d", len(actions.Messages))
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("media removed before late reader: %v", err)
	}
	restarted := newDeferredMediaQueue(handler.deferredMedia.root)
	if len(restarted.jobs) != 1 || len(restarted.readyJobs()) != 0 {
		t.Fatal("restart lost lease or queued a replay")
	}
	for i := 1; i < deferredMediaMaxJobs; i++ {
		if !restarted.push(&deferredMediaJob{}) {
			t.Fatal("premature quota rejection")
		}
	}
	if restarted.push(&deferredMediaJob{}) {
		t.Fatal("retained media did not count toward quota")
	}
	restarted.mu.Lock()
	restarted.pruneRetainedLocked(job.retainUntil.Add(time.Second))
	restarted.mu.Unlock()
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("expired owned resource was not removed")
	}
}

func TestUnconfirmedSubscriptionPushIsNotRepeatedOnNextCheck(t *testing.T) {
	actions := testkit.NewActions()
	current := Settings{Enabled: true, Subscriptions: []Subscription{workflowSubscription("one", "alpha")}}
	initializeWorkflow(actions, current.Subscriptions)
	actions.MessageErrors = []error{&rayleabot.ActionError{Code: "adapter.send_unconfirmed", Message: "fixture receipt timeout"}}
	handler := newWorkflowHandler(t, workflowPlatform("alpha", readySession("alpha")))
	first := handler.Check(t.Context(), actions, current)
	if IntScalar(first["sent"]) != 0 || !BoolScalar(first["degraded"]) {
		t.Fatalf("unconfirmed push reported success: %#v", first)
	}
	handler.Check(t.Context(), actions, current)
	if len(actions.Messages) != 1 {
		t.Fatal("unconfirmed subscription message automatically resent")
	}
}
