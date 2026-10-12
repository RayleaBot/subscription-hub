package plugin

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func managementSettingsEvent(config map[string]any, botID string) *rayleabot.EventContext {
	payload := map[string]any{}
	if botID != "" {
		payload["page_id"] = "subscriptions"
		payload["bot"] = map[string]any{"source_protocol": "onebot11", "bot_id": botID}
	}
	return &rayleabot.EventContext{Config: config, Event: rayleabot.Event{EventType: "management.action", SourceProtocol: "management", SourceAdapter: "management.ui", Payload: payload}}
}

func settingsFixture(t *testing.T) (*Handler, *testkit.Actions, map[string]any) {
	t.Helper()
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, workflowPlatform("fixture", readySession("fixture")))
	handler.actions = actions
	a, b := workflowSubscription("a", "fixture"), workflowSubscription("b", "fixture")
	a.BotID, b.BotID = "1001", "1002"
	other := workflowSubscription("other", "fixture")
	other.BotID = "1003"
	settings := Settings{Enabled: true, DeliveryMaxAgeMinutes: 45, Subscriptions: []Subscription{a, b, other}, Resolver: NormalizeResolverSettings(ResolverSettings{SuperAdminWhitelist: true, Targets: []ResolverTarget{
		{SourceProtocol: "onebot11", BotID: "1001", TargetType: "group", TargetID: "200", Bilibili: true},
		{SourceProtocol: "onebot11", BotID: "1002", TargetType: "group", TargetID: "200", Weibo: true},
		{SourceProtocol: "onebot11", BotID: "1003", TargetType: "private", TargetID: "300", Douyin: true},
	}})}
	encoded, _ := json.Marshal(settings)
	config := map[string]any{}
	if err := json.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	return handler, actions, config
}

func TestManagementAccountFilteringAndRequiredContext(t *testing.T) {
	handler, _, config := settingsFixture(t)
	event := managementSettingsEvent(config, "1001")
	current, err := handler.loadSettings(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	bot, err := managementBot(event)
	if err != nil {
		t.Fatal(err)
	}
	filtered := settingsForBot(current, bot)
	if len(filtered.Subscriptions) != 1 || filtered.Subscriptions[0].ID != "a" || len(filtered.Resolver.Targets) != 1 || filtered.Resolver.Targets[0].BotID != "1001" {
		t.Fatalf("unfiltered settings: %+v", filtered)
	}
	missing := managementSettingsEvent(config, "")
	missing.Event.Payload["payload"] = map[string]any{"bot": map[string]any{"source_protocol": "onebot11", "bot_id": "1001"}}
	if _, err := managementBot(missing); err == nil {
		t.Fatal("nested business bot accepted")
	}
	for _, action := range []string{"subscription.save", "resolver.save"} {
		if _, err := handler.saveManagementSettings(t.Context(), missing, action, nil); err == nil {
			t.Fatalf("%s accepted missing account", action)
		}
	}
	event.Event.Payload["bot"] = map[string]any{"source_protocol": "qqofficial", "bot_id": "opaque:id"}
	if _, err := managementBot(event); err == nil {
		t.Fatal("unsupported protocol accepted")
	}
}

func TestManagementRejectsForeignRecordsAndForgedOwnersAtomically(t *testing.T) {
	for _, action := range []string{"subscription.save", "resolver.save"} {
		for _, conflict := range []string{"bot_id", "source_protocol", "owner", "id"} {
			if action == "resolver.save" && conflict == "id" {
				continue
			}
			t.Run(action+"/"+conflict, func(t *testing.T) {
				handler, actions, config := settingsFixture(t)
				item := map[string]any{"target_type": "group", "target_id": "200"}
				collection := "targets"
				if action == "subscription.save" {
					collection = "subscriptions"
					item["platform"], item["uid"], item["id"] = "fixture", "subject", "a"
				}
				switch conflict {
				case "bot_id":
					item["bot_id"] = "1002"
				case "source_protocol":
					item["source_protocol"] = "qqofficial"
				case "owner":
					item["owner"] = "1002"
				case "id":
					item["id"] = "b"
				}
				if _, err := handler.saveManagementSettings(t.Context(), managementSettingsEvent(config, "1001"), action, map[string]any{collection: []any{item}}); err == nil {
					t.Fatal("foreign record accepted")
				}
				if len(actions.Config) != 0 {
					t.Fatal("invalid batch wrote settings")
				}
			})
		}
	}
}

func TestConcurrentAccountSavesKeepOtherAccountsAndGlobalSettings(t *testing.T) {
	handler, _, config := settingsFixture(t)
	var workers sync.WaitGroup
	for _, botID := range []string{"1001", "1002"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			event := managementSettingsEvent(config, botID) // Deliberately the same stale snapshot.
			item := map[string]any{"platform": "fixture", "uid": "new-" + botID, "target_type": "group", "target_id": "200", "enabled": true}
			if _, err := handler.saveManagementSettings(t.Context(), event, "subscription.save", map[string]any{"subscriptions": []any{item}}); err != nil {
				t.Error(err)
			}
			target := map[string]any{"target_type": "group", "target_id": "300", "douyin": true}
			if _, err := handler.saveManagementSettings(t.Context(), event, "resolver.save", map[string]any{"targets": []any{target}}); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	current, err := handler.loadSettings(t.Context(), managementSettingsEvent(config, "1001"))
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Subscriptions) != 3 || len(current.Resolver.Targets) != 3 || !current.Enabled || current.DeliveryMaxAgeMinutes != 45 || !current.Resolver.SuperAdminWhitelist {
		t.Fatalf("save lost data: %+v", current)
	}
	for _, botID := range []string{"1001", "1002"} {
		owned := settingsForBot(current, rayleabot.BotAccount{SourceProtocol: "onebot11", BotID: botID})
		if len(owned.Subscriptions) != 1 || owned.Subscriptions[0].UID != "new-"+botID || owned.Resolver.Targets[0].TargetID != "300" {
			t.Fatalf("lost account %s: %+v", botID, owned)
		}
	}
}

func TestDelayedConfigEchoCannotUndoNewerSave(t *testing.T) {
	handler, actions, config := settingsFixture(t)
	event := managementSettingsEvent(config, "1001")
	if _, err := handler.saveManagementSettings(t.Context(), event, "subscription.save", map[string]any{"subscriptions": []any{}}); err != nil {
		t.Fatal(err)
	}
	older, _ := json.Marshal(actions.Config)
	if _, err := handler.saveManagementSettings(t.Context(), managementSettingsEvent(config, "1002"), "subscription.save", map[string]any{"subscriptions": []any{}}); err != nil {
		t.Fatal(err)
	}
	var echo map[string]any
	_ = json.Unmarshal(older, &echo)
	change := &rayleabot.EventContext{Config: config, Event: rayleabot.Event{EventType: "config.changed", Payload: map[string]any{"config": echo, "changed_keys": []string{"subscriptions"}}}}
	if err := handler.settingsChanged(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	current, err := handler.loadSettings(t.Context(), event)
	if err != nil || len(current.Subscriptions) != 1 || current.Subscriptions[0].ID != "other" {
		t.Fatalf("echo undid newer save: %+v, %v", current, err)
	}
	// A genuine external global change still takes effect.
	change.Event.Payload = map[string]any{"config": map[string]any{"enabled": false}, "changed_keys": []string{"enabled"}}
	if err := handler.settingsChanged(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	current, _ = handler.loadSettings(t.Context(), event)
	if current.Enabled {
		t.Fatal("external change ignored")
	}
}

func TestGlobalStrategyPreservesAccountData(t *testing.T) {
	handler, _, config := settingsFixture(t)
	global := managementSettingsEvent(config, "")
	before, _ := handler.loadSettings(t.Context(), global)
	payload := map[string]any{"enabled": false, "delivery_max_age_minutes": 90, "super_admin_whitelist": false, "cooldowns": before.Resolver.Cooldowns, "media": before.Resolver.Media}
	if _, err := handler.saveManagementSettings(t.Context(), global, "strategy.save", payload); err != nil {
		t.Fatal(err)
	}
	current, _ := handler.loadSettings(t.Context(), global)
	if !reflect.DeepEqual(current.Subscriptions, before.Subscriptions) || !reflect.DeepEqual(current.Resolver.Targets, before.Resolver.Targets) {
		t.Fatal("strategy overwrote account records")
	}
	if _, err := handler.saveManagementSettings(t.Context(), managementSettingsEvent(config, "1001"), "strategy.save", payload); err == nil {
		t.Fatal("account page wrote global strategy")
	}
}
