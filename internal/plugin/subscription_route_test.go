package plugin

import (
	"context"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestScheduledSubscriptionsKeepAdapterForCardsAndMedia(t *testing.T) {
	actions := testkit.NewActions()
	first, second := workflowSubscription("first", "fixture"), workflowSubscription("second", "fixture")
	first.SourceAdapter, second.SourceAdapter = "bot-a", "bot-b"
	first.TargetID, second.TargetID = "10001", "10001"
	current := Settings{Enabled: true, Subscriptions: []Subscription{first, second}}
	initializeWorkflow(actions, current.Subscriptions)
	handler := newWorkflowHandler(t, workflowPlatform("fixture", readySession("fixture")))
	if got := handler.Check(t.Context(), actions, current); IntScalar(got["sent"]) != 2 {
		t.Fatalf("check=%v", got)
	}
	for i, item := range current.Subscriptions {
		if actions.Messages[i].SourceAdapter != item.SourceAdapter || actions.Messages[i].SourceProtocol != item.SourceProtocol {
			t.Fatalf("route lost: %+v", actions.Messages[i])
		}
		if subscriptionMediaTarget(item, nil).SourceAdapter != item.SourceAdapter {
			t.Fatal("media route lost")
		}
	}
	if len(handler.NormalizeSubscriptions(current.Subscriptions)) != 2 {
		t.Fatal("different adapters merged")
	}
	event := &rayleabot.EventContext{Event: rayleabot.Event{SourceProtocol: "onebot11", SourceAdapter: "bot-b", Target: rayleabot.Target{Type: "private", ID: "10001"}}}
	if subscriptionTargetsEvent(first, event) || !subscriptionTargetsEvent(second, event) {
		t.Fatal("subscription crosses adapter boundary")
	}
}

func TestSubscriptionMissingRouteIsRejectedBeforeRender(t *testing.T) {
	item := workflowSubscription("fixture", "fixture")
	item.SourceAdapter = ""
	if validateSettingsRoutes(Settings{Subscriptions: []Subscription{item}}) == nil {
		t.Fatal("missing route accepted")
	}
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t)
	failures := checkFailures{}
	if handler.deliverUpdate(t.Context(), t.Context(), actions, item, nil, CardRequest{}, nil, &failures) || len(actions.Renders) != 0 || len(actions.Messages) != 0 {
		t.Fatal("invalid route started delivery work")
	}
}

func TestCardRenderBudgetReservesResponseTime(t *testing.T) {
	actions := testkit.NewActions()
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	if _, err := RenderSubscriptionCardImage(ctx, actions, "fixture", map[string]any{}, "", nil); err != nil {
		t.Fatal(err)
	}
	if budget := actions.ResourceRenders[0].TimeoutMS; budget <= 0 || budget > 5000 {
		t.Fatalf("host budget=%d", budget)
	}
	short, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if _, err := RenderSubscriptionCardImage(short, actions, "fixture", map[string]any{}, "", nil); err == nil || len(actions.Renders) != 1 {
		t.Fatal("render started without finalization budget")
	}
}
