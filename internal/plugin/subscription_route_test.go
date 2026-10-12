package plugin

import (
	"context"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestScheduledSubscriptionsKeepAccountForCardsAndMedia(t *testing.T) {
	actions := testkit.NewActions()
	first, second := workflowSubscription("first", "fixture"), workflowSubscription("second", "fixture")
	first.BotID, second.BotID = "1001", "1002"
	first.TargetID, second.TargetID = "10001", "10001"
	current := Settings{Enabled: true, Subscriptions: []Subscription{first, second}}
	initializeWorkflow(actions, current.Subscriptions)
	handler := newWorkflowHandler(t, workflowPlatform("fixture", readySession("fixture")))
	if got := handler.Check(t.Context(), actions, current); IntScalar(got["sent"]) != 2 {
		t.Fatalf("check=%v", got)
	}
	for i, item := range current.Subscriptions {
		if actions.Messages[i].SourceAdapter != "" || actions.Messages[i].BotID != item.BotID || actions.Messages[i].SourceProtocol != item.SourceProtocol {
			t.Fatalf("route lost: %+v", actions.Messages[i])
		}
		if subscriptionMediaTarget(item, nil).Address != item.Address() {
			t.Fatal("media route lost")
		}
	}
	if len(handler.NormalizeSubscriptions(current.Subscriptions)) != 2 {
		t.Fatal("different accounts merged")
	}
	event := &rayleabot.EventContext{Event: rayleabot.Event{SourceProtocol: "onebot11", SourceAdapter: "new-connection", BotID: "1002", Target: rayleabot.Target{Type: "private", ID: "10001"}}}
	if subscriptionTargetsEvent(first, event) || !subscriptionTargetsEvent(second, event) {
		t.Fatal("subscription crosses account boundary")
	}
}

func TestSubscriptionMissingRouteIsRejectedBeforeRender(t *testing.T) {
	item := workflowSubscription("fixture", "fixture")
	item.BotID = ""
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

func TestAccountIdentitySurvivesAdapterReplacement(t *testing.T) {
	first := workflowSubscription("account-route", "fixture")
	event := &rayleabot.EventContext{Event: rayleabot.Event{SourceProtocol: "onebot11", BotID: first.BotID, SourceAdapter: "replacement", Target: rayleabot.Target{Type: first.TargetType, ID: first.TargetID}}}
	if !subscriptionTargetsEvent(first, event) {
		t.Fatal("replacement adapter broke the saved subscription")
	}
	second := first
	second.BotID = "1002"
	if subscriptionIdentity(first) == subscriptionIdentity(second) {
		t.Fatal("different accounts share identity")
	}
	target := ResolverTarget{SourceProtocol: first.SourceProtocol, BotID: first.BotID, TargetType: first.TargetType, TargetID: first.TargetID, Bilibili: true}
	settings := ResolverSettings{Targets: []ResolverTarget{target}}
	if found, ok := settings.Target(event.Event.SourceProtocol, event.Event.BotID, event.Event.Target.Type, event.Event.Target.ID); !ok || !found.Bilibili {
		t.Fatal("replacement adapter broke resolver binding")
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
