package bilibili

import (
	"context"
	"encoding/base64"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func TestResolveAvatarDataURLInlinesSupportedImage(t *testing.T) {
	fake := newActions()
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}}

	dataURL, size, err := plugin.ResolveAvatarDataURLWithTimeout(context.Background(), fake, "https://i2.hdslb.com/bfs/face/fixture.webp", 3, avatarPolicy())
	if err != nil {
		t.Fatalf("resolveAvatarDataURL() error = %v", err)
	}
	if size != len(body) || dataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(body) {
		t.Fatalf("unexpected resolved avatar: size=%d data=%q", size, dataURL)
	}
	if len(fake.HTTPRequests) != 1 || fake.HTTPRequests[0].Headers["Referer"] != "https://www.bilibili.com/" || fake.HTTPRequests[0].Headers["Cache-Control"] != "no-cache" || fake.HTTPRequests[0].Headers["Pragma"] != "no-cache" {
		t.Fatalf("unexpected avatar request: %#v", fake.HTTPRequests)
	}
}

func TestResolveAvatarDataURLRejectsUndeclaredShapesBeforeRequest(t *testing.T) {
	fake := newActions()
	for _, sourceURL := range []string{
		"http://i2.hdslb.com/bfs/face/fixture.webp",
		"https://i2.hdslb.com/bfs/archive/fixture.webp",
		"https://i2.hdslb.com:8443/bfs/face/fixture.webp",
		"https://q1.qlogo.cn/g?b=qq&nk=fixture&s=100",
		"https://p.qlogo.cn/gh/100/200/100",
		"https://static.hdslb.com/images/member/noface.gif",
		"https://static.hdslb.com/images/member/noface.gif?variant=1",
		"https://static.hdslb.com/images/member/other.gif",
		"https://example.test/avatar.png",
	} {
		if _, _, err := plugin.ResolveAvatarDataURLWithTimeout(context.Background(), fake, sourceURL, 3, avatarPolicy()); err == nil {
			t.Fatalf("resolveAvatarDataURL(%q) accepted unsupported URL", sourceURL)
		}
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("unsupported avatars reached http.request: %#v", fake.HTTPRequests)
	}
}
