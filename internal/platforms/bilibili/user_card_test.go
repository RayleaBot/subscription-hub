package bilibili

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestBuildBilibiliUserCardData(t *testing.T) {
	item := plugin.Subscription{
		Platform: "bilibili", UID: "42", Name: "测试 UP", AvatarURL: "https://i0.hdslb.com/face.jpg",
		TargetType: "group", TargetID: "100", TargetName: "测试群",
	}
	user := bilibiliUser{UID: "42", Name: "测试 UP", Fans: 1286000, Sign: strings.Repeat("简介", 60), Videos: 233, Level: 6, Senior: true, Verify: "bilibili 知名UP主"}
	data := buildBilibiliUserCardData("subscribed", item, user, []string{"live", "video"})
	if data["action"] != "subscribed" || data["platform"] != "Bilibili" {
		t.Fatalf("unexpected card head: %#v", data)
	}
	if data["uid_text"] != "UID 42" || data["fans_text"] != "粉丝 128.6万" {
		t.Fatalf("unexpected card identity: %#v", data)
	}
	if data["videos_text"] != "视频 233" || data["level_icon"] != "assets/lv6-senior.svg" || data["verify_text"] != "bilibili 知名UP主" || data["verify_org"] != false {
		t.Fatalf("unexpected card detail badges: %#v", data)
	}
	if data["services_text"] != "直播、视频" {
		t.Fatalf("unexpected services text: %#v", data["services_text"])
	}
	labels, ok := data["service_labels"].([]string)
	if !ok || len(labels) != 2 || labels[0] != "直播" || labels[1] != "视频" {
		t.Fatalf("unexpected service labels: %#v", data["service_labels"])
	}
	if data["target_text"] != "订阅到：测试群" {
		t.Fatalf("unexpected target text: %#v", data["target_text"])
	}
	cardUser := plugin.MapValue(data["user"])
	if plugin.StringScalar(cardUser["avatar"]) != "https://i0.hdslb.com/face.jpg" {
		t.Fatalf("avatar should fall back to stored value: %#v", cardUser)
	}
	if sign := plugin.StringScalar(cardUser["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 83 {
		t.Fatalf("sign was not truncated: %d runes", len([]rune(sign)))
	}
}

func TestBuildBilibiliUserCardDataUnsubscribed(t *testing.T) {
	item := plugin.Subscription{Platform: "bilibili", UID: "42", Name: "测试 UP", TargetType: "private", TargetID: "7"}
	data := buildBilibiliUserCardData("unsubscribed", item, bilibiliUser{}, []string{"all"})
	if data["action"] != "unsubscribed" || data["target_text"] != "取消于：私聊" {
		t.Fatalf("unexpected unsubscribe card: %#v", data)
	}
	if data["fans_text"] != "" || data["videos_text"] != "" || data["verify_text"] != "" || data["level_icon"] != "" {
		t.Fatalf("empty detail fields should stay empty: %#v", data)
	}
	if data["services_text"] != "全部" {
		t.Fatalf("unexpected services text: %#v", data["services_text"])
	}
}

func TestFansTextFormatting(t *testing.T) {
	for input, want := range map[int]string{0: "", -3: "", 9999: "粉丝 9999", 10000: "粉丝 1.0万", 1286000: "粉丝 128.6万"} {
		if got := plugin.FansText(input); got != want {
			t.Fatalf("fansText(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestRemoveSubscriptionOutcomeCarriesCardData(t *testing.T) {
	current := plugin.Settings{Subscriptions: []plugin.Subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "测试 UP", AvatarURL: "https://i0.hdslb.com/face.jpg",
		TargetType: "group", TargetID: "100", TargetName: "测试群", Services: []string{"all"}, Enabled: true,
	}}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"直播", "42"}},
	}}
	outcome := newHandler(t).RemoveSubscription(&current, event, "bilibili")
	if !outcome.Changed || outcome.Action != "unsubscribed" || outcome.Message != "已取消订阅：Bilibili 42（直播）" {
		t.Fatalf("unexpected remove outcome: %#v", outcome)
	}
	if outcome.Item == nil || outcome.Item.UID != "42" || outcome.Item.Name != "测试 UP" || outcome.Item.AvatarURL == "" {
		t.Fatalf("remove outcome lost card profile: %#v", outcome.Item)
	}
	if len(outcome.Services) != 1 || outcome.Services[0] != "live" {
		t.Fatalf("remove outcome lost removed services: %#v", outcome.Services)
	}
}

func TestInlineBilibiliCardAvatar(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	if got := inlineBilibiliCardAvatar(context.Background(), fake, "https://i0.hdslb.com/bfs/face/one.webp"); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("allowlisted avatar was not inlined: %q", got)
	}
	if len(fake.HTTPRequests) != 1 || !strings.HasSuffix(fake.HTTPRequests[0].URL, bilibiliSearchAvatarSuffix) {
		t.Fatalf("avatar did not request a compact thumbnail: %#v", fake.HTTPRequests)
	}
	for _, sourceURL := range []string{"", "https://static.hdslb.com/images/member/noface.gif", "https://example.test/face.jpg"} {
		if got := inlineBilibiliCardAvatar(context.Background(), fake, sourceURL); got != "" {
			t.Fatalf("inlineBilibiliCardAvatar(%q) = %q, want empty fallback", sourceURL, got)
		}
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("unsupported avatars reached http.request: %#v", fake.HTTPRequests)
	}
}

func TestInlineBilibiliCardAvatarRetriesOriginalSource(t *testing.T) {
	actions := &fallbackAvatarActions{Actions: newActions()}
	sourceURL := "https://i2.hdslb.com/bfs/face/fallback.webp"
	if got := inlineBilibiliCardAvatar(context.Background(), actions, sourceURL); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("original avatar retry failed: %q", got)
	}
	if len(actions.requests) != 2 || !strings.HasSuffix(actions.requests[0].URL, bilibiliSearchAvatarSuffix) || actions.requests[1].URL != sourceURL {
		t.Fatalf("unexpected avatar retry requests: %#v", actions.requests)
	}
}

func TestInlineBilibiliCardAvatarDoesNotRetryIdenticalSource(t *testing.T) {
	actions := &testkit.FailedAvatarActions{Actions: newActions()}
	sourceURL := "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"
	if got := inlineBilibiliCardAvatar(context.Background(), actions, sourceURL); got != "" {
		t.Fatalf("failed avatar should fall back to empty: %q", got)
	}
	if len(actions.Requests) != 1 || actions.Requests[0].URL != sourceURL {
		t.Fatalf("identical avatar candidates were requested more than once: %#v", actions.Requests)
	}
}

func TestBilibiliUserCardTemplatePreservesInlineAvatar(t *testing.T) {
	source, err := os.ReadFile(testkit.RepositoryPath(t, "templates/bilibili-user-card/template.html"))
	if err != nil {
		t.Fatalf("read user card template: %v", err)
	}
	compiled, err := template.New("bilibili-user-card").Parse(string(source))
	if err != nil {
		t.Fatalf("parse user card template: %v", err)
	}

	const avatar = "data:image/png;base64,fixture"
	data := buildBilibiliUserCardData("subscribed",
		plugin.Subscription{Platform: "bilibili", UID: "42", Name: "示例 UP", TargetType: "group", TargetID: "100", TargetName: "测试群"},
		bilibiliUser{UID: "42", Name: "示例 UP", AvatarURL: avatar, Fans: 1286000, Level: 6, Verify: "bilibili 知名UP主"},
		[]string{"all"})
	data["Stylesheet"] = template.CSS("")
	data["Theme"] = "default"
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute user card template: %v", err)
	}

	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") || !strings.Contains(html, `data-avatar="`+avatar+`"`) {
		t.Fatalf("inline avatar did not survive template escaping: %s", html)
	}
	for _, marker := range []string{`src="assets/bilibili-default-avatar.gif"`, "image.src = source", "lv-badge", "verify-chip", "订阅成功"} {
		if !strings.Contains(html, marker) {
			t.Fatalf("user card template missing %q: %s", marker, html)
		}
	}
}
