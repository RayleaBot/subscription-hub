package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestScheduledSubscriptionsKeepAccountForCardsAndMedia(t *testing.T) {
	actions := testkit.NewActions()
	first, second := workflowSubscription("first", "fixture"), workflowSubscription("second", "fixture")
	first.BotID, second.BotID = "1001", "1002"
	first.SourceAdapter, second.SourceAdapter = "old-a", "old-b"
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
	first := workflowSubscription("legacy-adapter-hash", "fixture")
	first.SourceAdapter = "removed"
	second := first
	second.SourceAdapter = "replacement"
	if subscriptionIdentity(first) != subscriptionIdentity(second) {
		t.Fatal("adapter participates in identity")
	}
	event := &rayleabot.EventContext{Event: rayleabot.Event{SourceProtocol: "onebot11", BotID: first.BotID, SourceAdapter: second.SourceAdapter, Target: rayleabot.Target{Type: first.TargetType, ID: first.TargetID}}}
	if !subscriptionTargetsEvent(first, event) {
		t.Fatal("replacement adapter broke the saved subscription")
	}
	second.BotID = "1002"
	if subscriptionIdentity(first) == subscriptionIdentity(second) {
		t.Fatal("different accounts share identity")
	}
	target := ResolverTarget{SourceProtocol: first.SourceProtocol, BotID: first.BotID, SourceAdapter: "removed", TargetType: first.TargetType, TargetID: first.TargetID, Bilibili: true}
	settings := ResolverSettings{Targets: []ResolverTarget{target}}
	if found, ok := settings.Target(event.Event.SourceProtocol, event.Event.BotID, event.Event.Target.Type, event.Event.Target.ID); !ok || !found.Bilibili {
		t.Fatal("replacement adapter broke resolver binding")
	}
	handler := newWorkflowHandler(t)
	cooldowns := NormalizeResolverSettings(ResolverSettings{Cooldowns: ResolverCooldownSettings{SameLinkEnabled: true}}).Cooldowns
	if handler.resolverCooldownHit(target, "bilibili", "same-link", cooldowns) {
		t.Fatal("first request hit cooldown")
	}
	target.SourceAdapter = "replacement"
	if !handler.resolverCooldownHit(target, "bilibili", "same-link", cooldowns) {
		t.Fatal("adapter replacement bypassed cooldown")
	}
}

func legacyRouteConfig(t *testing.T) map[string]any {
	t.Helper()
	var config map[string]any
	err := json.Unmarshal([]byte(`{
        "subscriptions": [
            {"id":"keep-old-adapter-id", "source_protocol":"onebot11", "source_adapter":"old", "platform":"fixture", "uid":"subject", "target_type":"private", "target_id":"100", "services":["all"], "enabled":true},
            {"id":"pending-a", "source_protocol":"onebot11", "source_adapter":"removed-a", "platform":"fixture", "uid":"subject", "target_type":"private", "target_id":"100", "enabled":true},
            {"id":"pending-b", "source_protocol":"qqofficial", "source_adapter":"old", "platform":"fixture", "uid":"subject", "target_type":"private", "target_id":"100", "enabled":true}
        ],
        "resolver":{"targets":[
            {"source_protocol":"onebot11", "source_adapter":"old", "target_type":"group", "target_id":"200", "bilibili":true},
            {"source_protocol":"onebot11", "source_adapter":"removed-a", "target_type":"group", "target_id":"200", "weibo":true},
            {"source_protocol":"onebot11", "source_adapter":"removed-b", "target_type":"group", "target_id":"200", "douyin":true}
        ]}
    }`), &config)
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func TestLoadMigratesRoutesWithoutChangingIDsOrSeenState(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, workflowPlatform("fixture", readySession("fixture")))
	handler.actions = actions
	event := &rayleabot.EventContext{Config: legacyRouteConfig(t), Bots: []rayleabot.Bot{{SourceProtocol: "onebot11", SourceAdapter: "old", ID: "1001"}}}
	current, err := handler.loadSettings(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Subscriptions) != 3 || len(current.Resolver.Targets) != 3 {
		t.Fatalf("lost legacy records: %+v", current)
	}
	migrated := current.Subscriptions[0]
	if migrated.ID != "keep-old-adapter-id" || migrated.BotID != "1001" || migrated.NeedsBotSelection {
		t.Fatalf("migration=%+v", migrated)
	}
	if !current.Subscriptions[1].NeedsBotSelection || !current.Subscriptions[2].NeedsBotSelection || current.Subscriptions[2].BotID != "" {
		t.Fatal("unresolved or other-protocol record was rebound")
	}
	if current.Resolver.Targets[0].BotID != "1001" || !current.Resolver.Targets[1].NeedsBotSelection || !current.Resolver.Targets[2].NeedsBotSelection {
		t.Fatal("resolver migration lost status")
	}
	// Resolved routes drop the adapter; unresolved ones keep it so a later identity snapshot can repair them.
	written := actions.Config["subscriptions"].([]Subscription)
	if written[0].SourceAdapter != "" || written[1].SourceAdapter != "removed-a" {
		t.Fatalf("written routes=%+v", written)
	}
	if err := handler.saveSettings(t.Context(), event, current); err != nil {
		t.Fatalf("pending record blocks saving: %v", err)
	}
	initializeWorkflow(actions, current.Subscriptions)
	update := workflowUpdate("fixture", "new")
	key := SubscriptionUpdateKey(migrated, update)
	if err := markUpdateSeen(t.Context(), actions, migrated, update, workflowNow); err != nil {
		t.Fatal(err)
	}
	current.Subscriptions[0].SourceAdapter = "replacement"
	current.Subscriptions[0].BotID = "1002"
	if SubscriptionUpdateKey(current.Subscriptions[0], update) != key {
		t.Fatal("rebinding changed seen key")
	}
	if got := handler.Check(t.Context(), actions, current); IntScalar(got["sent"]) != 0 || len(actions.Messages) != 0 {
		t.Fatalf("replayed seen update or delivered pending record: %v", got)
	}
	// The paused records must not be marked seen while another target is active.
	for _, item := range current.Subscriptions[1:] {
		if got, _ := actions.KVGet(t.Context(), SubscriptionUpdateKey(item, update)); got["exists"] == true {
			t.Fatalf("pending seen state advanced: %v", got)
		}
	}
}

type failingMigrationActions struct {
	*testkit.Actions
	fail bool
}

func (actions *failingMigrationActions) ConfigWrite(ctx context.Context, values map[string]any) (rayleabot.ActionResult, error) {
	if actions.fail {
		return nil, errors.New("fixture write failure")
	}
	return actions.Actions.ConfigWrite(ctx, values)
}

func TestFailedRouteMigrationRetainsConfigAndRetriesNextLoad(t *testing.T) {
	actions := &failingMigrationActions{Actions: testkit.NewActions(), fail: true}
	handler := newWorkflowHandler(t)
	handler.actions = actions
	event := &rayleabot.EventContext{Config: legacyRouteConfig(t), Bots: []rayleabot.Bot{{SourceProtocol: "onebot11", SourceAdapter: "old", ID: "1001"}}}
	before, _ := json.Marshal(event.Config)
	current, err := handler.loadSettings(t.Context(), event)
	after, _ := json.Marshal(event.Config)
	if err != nil || string(before) != string(after) || current.Subscriptions[0].BotID != "1001" || len(actions.Config) != 0 {
		t.Fatalf("failed migration lost settings: %v", err)
	}
	found := false
	for _, log := range actions.Logs {
		found = found || strings.Contains(log.Message, "fixture write failure")
	}
	if !found {
		t.Fatal("migration failure was hidden")
	}
	actions.fail = false
	if _, err := handler.loadSettings(t.Context(), event); err != nil || actions.Config["subscriptions"] == nil {
		t.Fatal("migration did not retry")
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
