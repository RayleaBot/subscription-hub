package weibo

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestResolveAvatarDataURLSupportsWeiboAvatarHosts(t *testing.T) {
	fake := testkit.NewActions()
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}}

	dataURL, _, err := plugin.ResolveAvatarDataURLWithTimeout(context.Background(), fake, "https://tva1.sinaimg.cn/crop.0.0.640.640.180/face.jpg", 3, avatarPolicy())
	if err != nil || !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("sinaimg avatar was not resolved: data=%q err=%v", dataURL, err)
	}
	if len(fake.HTTPRequests) != 1 || fake.HTTPRequests[0].Headers["Referer"] != "https://weibo.com/" {
		t.Fatalf("unexpected weibo avatar request: %#v", fake.HTTPRequests)
	}
	for _, host := range []string{"tva1", "tva4", "tvax1", "tvax4", "wx1", "wx4"} {
		if _, _, err := plugin.ValidateAvatarSourceURL("https://"+host+".sinaimg.cn/orj480/face.jpg", avatarPolicy()); err != nil {
			t.Fatalf("validateAvatarSourceURL(%s.sinaimg.cn) rejected a declared weibo host", host)
		}
	}

	for _, sourceURL := range []string{
		"http://tva1.sinaimg.cn/crop.0.0.640.640.180/face.jpg",
		"https://tva5.sinaimg.cn/crop.0.0.640.640.180/face.jpg",
		"https://n.sinaimg.cn/face.jpg",
	} {
		if _, _, err := plugin.ResolveAvatarDataURLWithTimeout(context.Background(), fake, sourceURL, 3, avatarPolicy()); err == nil {
			t.Fatalf("resolveAvatarDataURL(%q) accepted undeclared weibo host shape", sourceURL)
		}
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("undeclared weibo avatars reached http.request: %#v", fake.HTTPRequests)
	}
}

func TestInlineUpdateCardAvatarRejectsOversizedBody(t *testing.T) {
	fake := testkit.NewActions()
	body := make([]byte, plugin.MaxUpdateCardAvatarBytes+8)
	copy(body, []byte{0xff, 0xd8, 0xff, 0xe0})
	fake.HTTPResponses = []rayleabot.ActionResult{{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/jpeg"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}}
	got := plugin.InlineAvatar(context.Background(), fake, "https://wx4.sinaimg.cn/orj480/face.jpg", 3, plugin.MaxUpdateCardAvatarBytes, avatarPolicy())
	if got != "" {
		t.Fatalf("oversized weibo avatar was inlined: %q", got)
	}
}
