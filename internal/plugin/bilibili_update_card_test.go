package plugin

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestInlineBilibiliUpdateAvatars(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{avatarHTTPResult(), avatarHTTPResult(), avatarHTTPResult()}
	data := map[string]any{
		"author":   map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"},
		"original": map[string]any{"author": map[string]any{"name": "原作者", "avatar": "https://i1.hdslb.com/bfs/face/original.webp"}},
		"subscriber_cards": []any{
			map[string]any{"display_name": "柒柒", "avatar_url": "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"},
		},
	}

	inlineBilibiliUpdateAvatars(context.Background(), fake, data)

	if got := stringScalar(nestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("author avatar was not inlined: %q", got)
	}
	if got := stringScalar(nestedValue(data, "original", "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("original author avatar was not inlined: %q", got)
	}
	if got := stringScalar(nestedValue(data, "subscriber_cards", 0, "avatar_url")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("subscriber avatar was not inlined: %q", got)
	}
	if len(fake.httpRequests) != 3 {
		t.Fatalf("avatar requests = %#v", fake.httpRequests)
	}
}

func TestInlineBilibiliUpdateAvatarsKeepsLocalAssetsAndFallsBack(t *testing.T) {
	actions := &failedAvatarActions{fakePluginActions: newFakePluginActions()}
	data := map[string]any{
		"author": map[string]any{"name": "测试 UP", "avatar": "assets/avatar.svg"},
		"subscriber_cards": []any{
			map[string]any{"display_name": "柒柒", "avatar_url": "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"},
		},
	}

	inlineBilibiliUpdateAvatars(context.Background(), actions, data)

	if got := stringScalar(nestedValue(data, "author", "avatar")); got != "assets/avatar.svg" {
		t.Fatalf("local asset avatar should stay untouched: %q", got)
	}
	if got := stringScalar(nestedValue(data, "subscriber_cards", 0, "avatar_url")); got != "" {
		t.Fatalf("failed avatar should fall back to empty: %q", got)
	}
}

func TestInlineBilibiliUpdateAvatarsCoversProductionSubscriberCards(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{avatarHTTPResult(), avatarHTTPResult()}
	item := subscription{
		Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "100",
		Subscribers: []subscriber{{ID: "10000", Nickname: "柒柒"}},
	}
	update := map[string]any{
		"id": "dyn-1", "service": "video", "title": "测试视频", "pub_ts": 1700000000,
		"author": map[string]any{"name": "测试 UP", "uid": "123456", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"},
	}
	// buildSubscriberCards 返回 []map[string]any，这里按生产类型走完整构建链路。
	data := buildBilibiliRenderData(item, update)
	if _, ok := data["subscriber_cards"].([]map[string]any); !ok {
		t.Fatalf("subscriber_cards type changed, update the inliner accordingly: %T", data["subscriber_cards"])
	}

	inlineBilibiliUpdateAvatars(context.Background(), fake, data)

	if got := stringScalar(nestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("author avatar was not inlined: %q", got)
	}
	cards := data["subscriber_cards"].([]map[string]any)
	if got := stringScalar(cards[0]["avatar_url"]); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("subscriber avatar was not inlined: %q", got)
	}
}

func TestInlineBilibiliUpdateAvatarsSharesCacheAcrossCards(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{avatarHTTPResult()}
	cache := &bilibiliAvatarCache{}
	const sourceURL = "https://i0.hdslb.com/bfs/face/shared.webp"

	for index := range 2 {
		data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": sourceURL}}
		inlineBilibiliUpdateAvatarsWithCache(context.Background(), fake, data, cache)
		if got := stringScalar(nestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
			t.Fatalf("card %d did not receive the cached avatar: %q", index, got)
		}
	}
	if len(fake.httpRequests) != 1 {
		t.Fatalf("shared avatar generated %d requests, want 1", len(fake.httpRequests))
	}
}

func TestInlineBilibiliUpdateAvatarsUsesShortTotalBudget(t *testing.T) {
	fake := newFakePluginActions()
	fake.httpResponses = []rayleabot.ActionResult{avatarHTTPResult()}
	actions := &deadlineCapturingActions{fakePluginActions: fake}
	startedAt := time.Now()
	data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"}}

	inlineBilibiliUpdateAvatars(context.Background(), actions, data)

	if actions.deadline.IsZero() || actions.deadline.After(startedAt.Add(bilibiliUpdateAvatarTotalTimeout+time.Second)) {
		t.Fatalf("update avatar deadline exceeded the total budget: %s", actions.deadline.Sub(startedAt))
	}
	if len(fake.httpRequests) != 1 || fake.httpRequests[0].TimeoutSeconds != bilibiliUpdateAvatarTimeoutSeconds {
		t.Fatalf("unexpected update avatar request budget: %#v", fake.httpRequests)
	}
}

func TestInlineBilibiliUpdateAvatarsSkipsRequestsAfterBudgetExpires(t *testing.T) {
	fake := newFakePluginActions()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"}}

	inlineBilibiliUpdateAvatarsWithCache(ctx, fake, data, &bilibiliAvatarCache{})

	if len(fake.httpRequests) != 0 {
		t.Fatalf("expired avatar budget still sent requests: %#v", fake.httpRequests)
	}
	if got := stringScalar(nestedValue(data, "author", "avatar")); got != "" {
		t.Fatalf("expired avatar budget should use the template fallback: %q", got)
	}
}

func TestBilibiliUpdateTemplatePreservesInlineAvatars(t *testing.T) {
	source, err := os.ReadFile("../../templates/bilibili-update/template.html")
	if err != nil {
		t.Fatalf("read update template: %v", err)
	}
	compiled, err := template.New("bilibili-update").Funcs(template.FuncMap{
		"safeHTML": func(value any) template.HTML { return template.HTML(stringScalar(value)) },
	}).Parse(string(source))
	if err != nil {
		t.Fatalf("parse update template: %v", err)
	}

	const avatar = "data:image/png;base64,fixture"
	data := map[string]any{
		"Stylesheet": template.CSS(""),
		"Theme":      "default",
		"service":    "视频", "category": "视频动态", "title": "测试动态",
		"author":   map[string]any{"name": "测试 UP", "avatar": avatar},
		"original": map[string]any{"title": "原动态", "author": map[string]any{"name": "原作者", "avatar": avatar}},
		"subscriber_cards": []map[string]any{
			{"display_name": "柒柒", "avatar_url": avatar},
		},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute update template: %v", err)
	}

	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") {
		t.Fatalf("inline avatar was escaped: %s", html)
	}
	if count := strings.Count(html, `data-avatar="`+avatar+`"`); count != 3 {
		t.Fatalf("expected 3 inlined avatars, got %d: %s", count, html)
	}
	for _, marker := range []string{"image.src = source", `data-fallback="assets/bilibili-default-avatar.gif"`, `src="assets/bilibili-default-avatar.gif"`} {
		if !strings.Contains(html, marker) {
			t.Fatalf("update template missing %q: %s", marker, html)
		}
	}
}
