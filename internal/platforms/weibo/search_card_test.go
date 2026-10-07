package weibo

import (
	"context"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestBuildWeiboSearchCardData(t *testing.T) {
	users := []weiboUser{
		{
			UID: "6000000001", Name: "示例博主", FansText: "粉丝 1286.5万",
			Verify: "微博认证：知名科技博主", Sign: strings.Repeat("简介", 40),
		},
		{UID: "6000000002", Name: "二号博主"},
	}

	data := buildWeiboSearchCardData(" 测试博主 ", users, []string{"*"})
	if data["query"] != "测试博主" || data["platform"] != "微博" || data["subtitle"] != "微博 · 订阅中心" || data["count"] != 2 {
		t.Fatalf("unexpected card head: %#v", data)
	}
	cards, ok := data["users"].([]map[string]any)
	if !ok || len(cards) != 2 {
		t.Fatalf("unexpected users payload: %#v", data["users"])
	}
	first := cards[0]
	if first["rank"] != 1 || first["uid_text"] != "UID 6000000001" || first["fans_text"] != "粉丝 1286.5万" {
		t.Fatalf("unexpected first card meta: %#v", first)
	}
	if first["verify_text"] != "微博认证：知名科技博主" || first["verify_org"] != false {
		t.Fatalf("unexpected first card badges: %#v", first)
	}
	if sign := plugin.StringScalar(first["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 51 {
		t.Fatalf("sign was not truncated: %d runes", len([]rune(sign)))
	}
	if cards[1]["fans_text"] != "" || cards[1]["verify_text"] != "" || cards[1]["verify_org"] != false || cards[1]["sign"] != "" {
		t.Fatalf("empty fields should stay empty: %#v", cards[1])
	}
	if data["footer_hint"] != "使用前缀 *：*订阅微博推送 [类型] UID或昵称" {
		t.Fatalf("footer should use the active command prefix: %#v", data["footer_hint"])
	}
}

func TestPrepareWeiboSearchAvatarsInlinesSinaimg(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	users := []weiboUser{{UID: "1", AvatarURL: "https://tva1.sinaimg.cn/crop.0.0.640.640.180/face.jpg"}}

	resolved := prepareWeiboSearchAvatars(context.Background(), fake, users)
	if len(resolved) != 1 || !strings.HasPrefix(resolved[0].AvatarURL, "data:image/png;base64,") {
		t.Fatalf("sinaimg avatar was not inlined: %#v", resolved)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("avatar requests = %#v", fake.HTTPRequests)
	}
	request := fake.HTTPRequests[0]
	if request.Headers["Referer"] != "https://weibo.com/" || request.TimeoutSeconds != weiboSearchAvatarTimeoutSeconds {
		t.Fatalf("unexpected avatar request: %#v", request)
	}
	if users[0].AvatarURL == "" {
		t.Fatal("avatar resolution mutated the search result source")
	}
}

func TestPrepareWeiboSearchAvatarsFallsBackForUnsupportedSources(t *testing.T) {
	fake := testkit.NewActions()
	resolved := prepareWeiboSearchAvatars(context.Background(), fake, []weiboUser{
		{UID: "1", AvatarURL: "https://example.test/face.jpg"},
		{UID: "2", AvatarURL: "http://tva1.sinaimg.cn/face.jpg"},
		{UID: "3"},
	})
	for index, user := range resolved {
		if user.AvatarURL != weiboSearchFallbackAvatar {
			t.Fatalf("avatar %d did not use the template asset: %#v", index, user)
		}
	}
	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("unsupported avatars reached http.request: %#v", fake.HTTPRequests)
	}
}

func TestPrepareWeiboSearchAvatarsUsesFallbackAfterFetchFailure(t *testing.T) {
	actions := &testkit.FailedAvatarActions{Actions: testkit.NewActions()}
	resolved := prepareWeiboSearchAvatars(context.Background(), actions, []weiboUser{
		{UID: "1", AvatarURL: "https://tvax2.sinaimg.cn/crop.0.0.640.640.180/face.jpg"},
	})
	if len(resolved) != 1 || resolved[0].AvatarURL != weiboSearchFallbackAvatar {
		t.Fatalf("failed avatar did not use the template asset: %#v", resolved)
	}
	if len(actions.Requests) != 1 {
		t.Fatalf("avatar attempts = %d, want 1", len(actions.Requests))
	}
}

func TestWeiboSubscribeHintFallsBackWithoutConfiguredPrefix(t *testing.T) {
	if got := weiboSubscribeHint([]string{"", "  "}); got != "使用“前缀 + 订阅微博推送 [类型] UID或昵称”" {
		t.Fatalf("unexpected prefix-neutral hint: %q", got)
	}
}
