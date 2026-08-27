package plugin

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestResolveAvatarDataURLsDeduplicatesBatchAndReportsFailures(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"content-type": "image/webp; charset=binary"},
		"body_base64": base64.StdEncoding.EncodeToString([]byte("fixture-webp")),
	}}
	sourceURL := "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"
	result := newWorkflowHandler(t).ResolveAvatarDataURLs(context.Background(), fake, map[string]any{
		"urls": []any{sourceURL, sourceURL, "https://example.test/avatar.png"},
	})
	items, ok := result["items"].([]resolvedAvatar)
	if !ok || len(items) != 1 || items[0].SourceURL != sourceURL || !strings.HasPrefix(items[0].DataURL, "data:image/webp;base64,") {
		t.Fatalf("unexpected resolved batch items: %#v", result["items"])
	}
	issues, ok := result["issues"].([]map[string]any)
	if !ok || len(issues) != 1 || issues[0]["message"] != "头像地址不受支持" {
		t.Fatalf("unexpected resolved batch issues: %#v", result["issues"])
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("duplicate avatar generated %d requests", len(fake.HTTPRequests))
	}
}
