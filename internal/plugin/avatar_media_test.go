package plugin

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestResolveAvatarDataURLInlinesSupportedImage(t *testing.T) {
	fake := newFakePluginActions()
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	fake.httpResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}}

	dataURL, size, err := resolveAvatarDataURL(context.Background(), fake, "https://i2.hdslb.com/bfs/face/fixture.webp")
	if err != nil {
		t.Fatalf("resolveAvatarDataURL() error = %v", err)
	}
	if size != len(body) || dataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(body) {
		t.Fatalf("unexpected resolved avatar: size=%d data=%q", size, dataURL)
	}
	if len(fake.httpRequests) != 1 || fake.httpRequests[0].Headers["Referer"] != "https://www.bilibili.com/" || fake.httpRequests[0].Headers["Cache-Control"] != "no-cache" || fake.httpRequests[0].Headers["Pragma"] != "no-cache" {
		t.Fatalf("unexpected avatar request: %#v", fake.httpRequests)
	}
}

func TestResolveAvatarDataURLRejectsUndeclaredShapesBeforeRequest(t *testing.T) {
	fake := newFakePluginActions()
	for _, sourceURL := range []string{
		"http://i2.hdslb.com/bfs/face/fixture.webp",
		"https://i2.hdslb.com/bfs/archive/fixture.webp",
		"https://i2.hdslb.com:8443/bfs/face/fixture.webp",
		"https://q1.qlogo.cn/g?b=qq&nk=fixture&s=100",
		"https://p.qlogo.cn/gh/100/200/100",
		"https://example.test/avatar.png",
	} {
		if _, _, err := resolveAvatarDataURL(context.Background(), fake, sourceURL); err == nil {
			t.Fatalf("resolveAvatarDataURL(%q) accepted unsupported URL", sourceURL)
		}
	}
	if len(fake.httpRequests) != 0 {
		t.Fatalf("unsupported avatars reached http.request: %#v", fake.httpRequests)
	}
}

func TestResolveAvatarDataURLsDeduplicatesBatchAndReportsFailures(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"content-type": "image/webp; charset=binary"},
		"body_base64": base64.StdEncoding.EncodeToString([]byte("fixture-webp")),
	}}
	sourceURL := "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"
	result := resolveAvatarDataURLs(context.Background(), fake, map[string]any{
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
	if len(fake.httpRequests) != 1 {
		t.Fatalf("duplicate avatar generated %d requests", len(fake.httpRequests))
	}
}
