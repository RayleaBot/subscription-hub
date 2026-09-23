package plugin

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestRuntimeAliasesAreDeclaredOnTheirCanonicalCommand(t *testing.T) {
	data, err := os.ReadFile(testkit.RepositoryPath(t, "info.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Commands []struct {
			Name    string
			Trigger struct{ Names []string }
		}
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for _, command := range manifest.Commands {
		for _, alias := range command.Trigger.Names {
			owners[alias] = command.Name
		}
	}
	for canonical, aliases := range commandAliasGroups {
		for _, alias := range aliases {
			if owners[alias] != canonical {
				t.Errorf("alias %q must be declared on %q", alias, canonical)
			}
		}
	}
}

func TestCommandAliasCollisionDoesNotReplaceExistingRoute(t *testing.T) {
	commands := map[string]commandRoute{
		"订阅状态": {operation: "status"}, "查看订阅状态": {platform: "fixture", operation: "search"},
	}
	if err := addCommandAliases(commands); err == nil {
		t.Fatal("conflicting alias was accepted")
	}
	if commands["查看订阅状态"].operation != "search" {
		t.Fatal("existing route was replaced")
	}
}
