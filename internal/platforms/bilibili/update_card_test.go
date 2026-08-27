package bilibili

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestInlineBilibiliUpdateAvatars(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult(), testkit.AvatarHTTPResult(), testkit.AvatarHTTPResult()}
	data := map[string]any{
		"author":   map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"},
		"original": map[string]any{"author": map[string]any{"name": "原作者", "avatar": "https://i1.hdslb.com/bfs/face/original.webp"}},
		"subscriber_cards": []any{
			map[string]any{"display_name": "柒柒", "avatar_url": "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"},
		},
	}

	newHandler(t).InlineUpdateAvatars(context.Background(), fake, data)

	if got := plugin.StringScalar(plugin.NestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("author avatar was not inlined: %q", got)
	}
	if got := plugin.StringScalar(plugin.NestedValue(data, "original", "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("original author avatar was not inlined: %q", got)
	}
	if got := plugin.StringScalar(plugin.NestedValue(data, "subscriber_cards", 0, "avatar_url")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("subscriber avatar was not inlined: %q", got)
	}
	if len(fake.HTTPRequests) != 3 {
		t.Fatalf("avatar requests = %#v", fake.HTTPRequests)
	}
}

func TestInlineBilibiliUpdateAvatarsKeepsLocalAssetsAndFallsBack(t *testing.T) {
	actions := &testkit.FailedAvatarActions{Actions: newActions()}
	data := map[string]any{
		"author": map[string]any{"name": "测试 UP", "avatar": "assets/avatar.svg"},
		"subscriber_cards": []any{
			map[string]any{"display_name": "柒柒", "avatar_url": "https://q1.qlogo.cn/g?b=qq&nk=10000&s=100"},
		},
	}

	newHandler(t).InlineUpdateAvatars(context.Background(), actions, data)

	if got := plugin.StringScalar(plugin.NestedValue(data, "author", "avatar")); got != "assets/avatar.svg" {
		t.Fatalf("local asset avatar should stay untouched: %q", got)
	}
	if got := plugin.StringScalar(plugin.NestedValue(data, "subscriber_cards", 0, "avatar_url")); got != "" {
		t.Fatalf("failed avatar should fall back to empty: %q", got)
	}
}

func TestInlineBilibiliUpdateAvatarsCoversProductionSubscriberCards(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult(), testkit.AvatarHTTPResult()}
	item := plugin.Subscription{
		Platform: "bilibili", UID: "123456", Name: "测试 UP", TargetType: "group", TargetID: "100",
		Subscribers: []plugin.Subscriber{{ID: "10000", Nickname: "柒柒"}},
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

	newHandler(t).InlineUpdateAvatars(context.Background(), fake, data)

	if got := plugin.StringScalar(plugin.NestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("author avatar was not inlined: %q", got)
	}
	cards := data["subscriber_cards"].([]map[string]any)
	if got := plugin.StringScalar(cards[0]["avatar_url"]); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("subscriber avatar was not inlined: %q", got)
	}
}

func TestInlineBilibiliUpdateAvatarsSharesCacheAcrossCards(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	cache := &plugin.AvatarCache{}
	const sourceURL = "https://i0.hdslb.com/bfs/face/shared.webp"

	for index := range 2 {
		data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": sourceURL}}
		newHandler(t).InlineUpdateAvatarsWithCache(context.Background(), fake, data, cache)
		if got := plugin.StringScalar(plugin.NestedValue(data, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
			t.Fatalf("card %d did not receive the cached avatar: %q", index, got)
		}
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("shared avatar generated %d requests, want 1", len(fake.HTTPRequests))
	}
}

func TestInlineBilibiliUpdateAvatarsGivesEachCardAFreshBudget(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	cache := &plugin.AvatarCache{}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	first := map[string]any{"author": map[string]any{"avatar": "https://i0.hdslb.com/bfs/face/first.webp"}}
	newHandler(t).InlineUpdateAvatars(expired, fake, first, cache)

	second := map[string]any{"author": map[string]any{"avatar": "https://i0.hdslb.com/bfs/face/second.webp"}}
	newHandler(t).InlineUpdateAvatars(context.Background(), fake, second, cache)

	if got := plugin.StringScalar(plugin.NestedValue(second, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("later card inherited an expired avatar budget: %q", got)
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("fresh card avatar requests = %#v", fake.HTTPRequests)
	}
}

func TestInlineBilibiliUpdateAvatarsDoesNotCacheFailures(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{
		{"status_code": 403},
		testkit.AvatarHTTPResult(),
	}
	cache := &plugin.AvatarCache{}
	const sourceURL = "https://q1.qlogo.cn/g?b=qq&nk=2678980697&s=100"
	first := map[string]any{"author": map[string]any{"avatar": sourceURL}}
	newHandler(t).InlineUpdateAvatarsWithCache(context.Background(), fake, first, cache)
	if got := plugin.StringScalar(plugin.NestedValue(first, "author", "avatar")); got != "" {
		t.Fatalf("failed avatar should use the template fallback: %q", got)
	}

	second := map[string]any{"author": map[string]any{"avatar": sourceURL}}
	newHandler(t).InlineUpdateAvatars(context.Background(), fake, second, cache)
	if got := plugin.StringScalar(plugin.NestedValue(second, "author", "avatar")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("later card did not retry a transient avatar failure: %q", got)
	}
	if len(fake.HTTPRequests) != 2 {
		t.Fatalf("avatar retry requests = %#v", fake.HTTPRequests)
	}
}

func TestBilibiliRenderDataFillsPartialUpdateAuthorFromSubscription(t *testing.T) {
	data := buildBilibiliRenderData(plugin.Subscription{
		UID: "123456", Name: "测试 UP", AvatarURL: "https://i0.hdslb.com/bfs/face/stored.webp",
	}, map[string]any{
		"service": "live", "title": "直播中",
		"author": map[string]any{"name": "测试 UP"},
	})
	author := plugin.MapValue(data["author"])
	if plugin.StringScalar(author["uid"]) != "123456" || plugin.StringScalar(author["avatar"]) != "https://i0.hdslb.com/bfs/face/stored.webp" {
		t.Fatalf("partial author lost the subscription identity: %#v", author)
	}
}

func TestInlineBilibiliUpdateAvatarsUsesShortTotalBudget(t *testing.T) {
	fake := newActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	actions := &testkit.DeadlineActions{Actions: fake}
	startedAt := time.Now()
	data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"}}

	newHandler(t).InlineUpdateAvatars(context.Background(), actions, data)

	if actions.Deadline.IsZero() || actions.Deadline.After(startedAt.Add(plugin.UpdateAvatarTotalTimeout+time.Second)) {
		t.Fatalf("update avatar deadline exceeded the total budget: %s", actions.Deadline.Sub(startedAt))
	}
	if len(fake.HTTPRequests) != 1 || fake.HTTPRequests[0].TimeoutSeconds != plugin.UpdateAvatarTimeoutSeconds {
		t.Fatalf("unexpected update avatar request budget: %#v", fake.HTTPRequests)
	}
}

func TestInlineBilibiliUpdateAvatarsSkipsRequestsAfterBudgetExpires(t *testing.T) {
	fake := newActions()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data := map[string]any{"author": map[string]any{"name": "测试 UP", "avatar": "https://i0.hdslb.com/bfs/face/author.webp"}}

	newHandler(t).InlineUpdateAvatarsWithCache(ctx, fake, data, &plugin.AvatarCache{})

	if len(fake.HTTPRequests) != 0 {
		t.Fatalf("expired avatar budget still sent requests: %#v", fake.HTTPRequests)
	}
	if got := plugin.StringScalar(plugin.NestedValue(data, "author", "avatar")); got != "" {
		t.Fatalf("expired avatar budget should use the template fallback: %q", got)
	}
}

func TestBilibiliUpdateTemplatePreservesInlineAvatars(t *testing.T) {
	source, err := os.ReadFile(testkit.RepositoryPath(t, "templates/bilibili-update/template.html"))
	if err != nil {
		t.Fatalf("read update template: %v", err)
	}
	compiled, err := template.New("bilibili-update").Funcs(template.FuncMap{
		"safeHTML": func(value any) template.HTML { return template.HTML(plugin.StringScalar(value)) },
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
