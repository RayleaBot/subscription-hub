package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/httpaction"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
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

func TestResolveAvatarDataURLsRetriesPolicyCandidates(t *testing.T) {
	platform := workflowPlatform("douyin", &workflowSession{})
	platform.Avatar = AvatarPolicy{
		Validate: func(parsed *url.URL) (string, bool) {
			return "https://www.douyin.com/", strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".douyinpic.com")
		},
		Candidates: func(parsed *url.URL) []string {
			mirror := strings.Replace(parsed.String(), "p3-pc", "p6-pc", 1)
			return []string{parsed.String(), mirror}
		},
	}
	handler := newWorkflowHandler(t, platform)
	fake := testkit.NewActions()
	fake.HTTPFallback = func(request httpaction.Request) (rayleabot.ActionResult, error, bool) {
		if strings.Contains(request.URL, "p3-pc") {
			return nil, errors.New("region p3 unavailable"), true
		}
		return testkit.AvatarHTTPResult(), nil, true
	}
	sourceURL := "https://p3-pc-sign.douyinpic.com/aweme/face.jpeg?x-expires=1780905600&x-signature=abc"
	result := handler.ResolveAvatarDataURLs(context.Background(), fake, map[string]any{"urls": []any{sourceURL}})
	items, ok := result["items"].([]resolvedAvatar)
	if !ok || len(items) != 1 || !strings.HasPrefix(items[0].DataURL, "data:image/png;base64,") {
		t.Fatalf("candidate retry did not produce a data URL: %#v", result)
	}
	if len(fake.HTTPRequests) < 2 {
		t.Fatalf("expected primary and mirror attempts, got %d requests", len(fake.HTTPRequests))
	}
}

type concurrentAvatarActions struct {
	*testkit.Actions
	mu       sync.Mutex
	started  int
	want     int
	timeouts []int
	release  chan struct{}
}

func newConcurrentAvatarActions(want int) *concurrentAvatarActions {
	return &concurrentAvatarActions{Actions: testkit.NewActions(), want: want, release: make(chan struct{})}
}

func (actions *concurrentAvatarActions) HTTPRequest(ctx context.Context, request httpaction.Request) (rayleabot.ActionResult, error) {
	actions.mu.Lock()
	actions.started++
	actions.timeouts = append(actions.timeouts, request.TimeoutSeconds)
	if actions.started == actions.want {
		close(actions.release)
	}
	actions.mu.Unlock()
	select {
	case <-actions.release:
		return testkit.AvatarHTTPResult(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestResolveAvatarDataURLsLoadsBatchConcurrently(t *testing.T) {
	const count = 4
	actions := newConcurrentAvatarActions(count)
	urls := make([]any, 0, count)
	for index := 0; index < count; index++ {
		urls = append(urls, "https://q1.qlogo.cn/g?b=qq&nk="+fmt.Sprint(10000+index)+"&s=100")
	}
	result := newWorkflowHandler(t).ResolveAvatarDataURLs(context.Background(), actions, map[string]any{"urls": urls})
	items, ok := result["items"].([]resolvedAvatar)
	if !ok || len(items) != count {
		t.Fatalf("concurrent avatar batch = %#v", result)
	}
	for index, item := range items {
		if item.SourceURL != urls[index] || !strings.HasPrefix(item.DataURL, "data:image/png;base64,") {
			t.Fatalf("avatar %d = %#v", index, item)
		}
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	for index, timeout := range actions.timeouts {
		if timeout < 8 {
			t.Fatalf("avatar %d timeout = %d seconds, want enough budget for slower CDN responses", index, timeout)
		}
	}
}
