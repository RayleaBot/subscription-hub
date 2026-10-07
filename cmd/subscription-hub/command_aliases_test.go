package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestManifestCommandAliasesRouteToTheSameOperation(t *testing.T) {
	data, err := os.ReadFile(testkit.RepositoryPath(t, "info.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Commands []struct {
			ID, Name, Permission string
			Trigger              struct {
				Type  string
				Names []string
			}
		}
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(t)
	owners := map[string]string{}
	for _, command := range manifest.Commands {
		platform, operation := handler.CommandOperation(command.Name)
		if command.Trigger.Type != "exact" || len(command.Trigger.Names) == 0 || operation == "" {
			t.Fatalf("unroutable manifest command: %#v", command)
		}
		wantPermission := "everyone"
		switch operation {
		case "add", "remove", "list_all", "check", "preview", "resolver_enable_bilibili", "resolver_disable_bilibili", "resolver_enable_weibo", "resolver_disable_weibo", "resolver_enable_douyin", "resolver_disable_douyin", "resolver_enable_super_admin", "resolver_disable_super_admin":
			wantPermission = "super_admin"
		}
		if command.Permission != wantPermission {
			t.Fatalf("%s permission = %q, want %q", command.ID, command.Permission, wantPermission)
		}
		for _, name := range command.Trigger.Names {
			if previous, exists := owners[name]; exists {
				t.Fatalf("%q belongs to both %s and %s", name, previous, command.ID)
			}
			owners[name] = command.ID
			gotPlatform, gotOperation := handler.CommandOperation(name)
			if gotPlatform != platform || gotOperation != operation {
				t.Errorf("alias %q = %s/%s, want %s/%s", name, gotPlatform, gotOperation, platform, operation)
			}
		}
	}
	for _, name := range []string{"订阅", "取消订阅", "搜索", "开启解析", "关闭解析", "抖音订阅全部", "检查订阅更新更多"} {
		if _, operation := handler.CommandOperation(name); operation != "" {
			t.Errorf("undeclared or ambiguous command %q was accepted", name)
		}
	}
}
