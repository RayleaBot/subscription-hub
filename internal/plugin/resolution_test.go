package plugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestManagementResolutionRequiresAnExactMatch(t *testing.T) {
	user := User{UID: "100", Name: "Fixture Official", Profile: map[string]any{"uid": "100", "name": "Fixture Official"}}
	session := &workflowSession{resolve: Resolution{Candidates: []User{user}}}
	handler := newWorkflowHandler(t, workflowPlatform("fixture", session))
	result := handler.ResolveUser(t.Context(), testkit.NewActions(), "fixture", "Fixture", Settings{})
	if result["exact"] != false {
		t.Fatalf("approximate candidate was marked exact: %#v", result)
	}
	if _, exists := result["user"]; exists {
		t.Fatalf("approximate candidate became the resolved user: %#v", result)
	}
	if len(result["candidates"].([]any)) != 1 {
		t.Fatalf("candidate was discarded: %#v", result)
	}
}

func TestResolutionFailuresCarrySourceAndStageWithoutCredentials(t *testing.T) {
	session := &workflowSession{resolveErr: errors.New("SUB=fixture-secret; lookup failed")}
	handler := newWorkflowHandler(t, workflowPlatform("fixture", session))
	actions := testkit.NewActions()
	resolved := handler.ResolveUser(t.Context(), actions, "fixture", "subject", Settings{})
	outcome := handler.AddSubscription(t.Context(), actions, &Settings{}, testkit.SubscriptionEvent("subject"), "fixture")
	for _, message := range []string{StringScalar(resolved["message"]), outcome.Message} {
		if !strings.Contains(message, "[fixture/resolve]") || strings.Contains(message, "fixture-secret") {
			t.Fatalf("unclassified or unsanitized resolve failure: %q", message)
		}
	}
}

func TestManagementResolutionReturnsTheExactCandidate(t *testing.T) {
	users := []User{
		{UID: "100", Name: "Fixture Official", Profile: map[string]any{"uid": "100", "name": "Fixture Official"}},
		{UID: "200", Name: "Fixture", Profile: map[string]any{"uid": "200", "name": "Fixture"}},
	}
	session := &workflowSession{resolve: Resolution{Candidates: users, Matched: &users[1]}}
	handler := newWorkflowHandler(t, workflowPlatform("fixture", session))
	result := handler.ResolveUser(t.Context(), testkit.NewActions(), "fixture", "fixture", Settings{})
	if result["exact"] != true || StringScalar(NestedValue(result, "user", "uid")) != "200" {
		t.Fatalf("exact candidate was not returned: %#v", result)
	}
}
