package douyin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/httpaction"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestShortLinkStopsAtContentRedirectWithoutRegisteringOrReadingLandingPage(t *testing.T) {
	for _, path := range []string{"video/7000000000000000001", "note/7000000000000000002"} {
		t.Run(path, func(t *testing.T) {
			fake := testkit.NewActions()
			landing := "https://www.iesdouyin.com/share/" + path + "/"
			fake.HTTPRoutes = []testkit.HTTPRoute{
				{Path: "/fixture/", Result: rayleabot.ActionResult{"status_code": 302, "headers": map[string]any{"Location": "/redirected/"}}},
				{Path: "/redirected/", Result: rayleabot.ActionResult{"status_code": 302, "headers": map[string]any{"Location": landing}}},
			}
			fake.HTTPFallback = func(request httpaction.Request) (rayleabot.ActionResult, error, bool) {
				return nil, fmt.Errorf("unexpected request to %s", request.URL), true
			}
			body, resolved, err := newDouyinClient(fake).requestShareHTML(t.Context(), "https://v.douyin.com/fixture/")
			if err != nil || resolved != landing || body != "" || len(fake.HTTPRequests) != 2 {
				t.Fatalf("short-link result = %q, %q, %v; requests=%v", body, resolved, err, testkit.RequestURLs(fake))
			}
		})
	}
}

func TestShortLinkDoesNotFollowUntrustedContentRedirect(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPResponses = []rayleabot.ActionResult{{"status_code": 302, "headers": map[string]any{"Location": "https://example.com/video/7000000000000000001"}}}
	_, _, err := newDouyinClient(fake).requestShareHTML(t.Context(), "https://v.douyin.com/fixture/")
	if err == nil || len(fake.HTTPRequests) != 1 {
		t.Fatalf("untrusted redirect = %v; requests=%v", err, testkit.RequestURLs(fake))
	}
}

func TestDouyinNetworkFailuresKeepTheirCauseWithoutLeakingRequestURL(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		kind, hint string
	}{
		{"dns", &net.DNSError{Err: "no such host", Name: "v.douyin.com"}, "dns", "域名解析失败"},
		{"timeout", context.DeadlineExceeded, "timeout", "超时"},
		{"canceled", context.Canceled, "canceled", "取消"},
		{"large response", httpaction.ErrResponseTooLarge, "response_too_large", "读取上限"},
		{"connection", errors.New("connection refused"), "network", "网络连接失败"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := &url.Error{Op: "Get", URL: "https://v.douyin.com/fixture/?token=fixture-only-secret", Err: test.err}
			fields := douyinErrorLogFields(err)
			if fields["kind"] != test.kind {
				t.Fatalf("failure classification = %#v", fields)
			}
			message := friendlyDouyinSourceError("抖音短链展开失败", err)
			if !strings.Contains(message, test.hint) || strings.Contains(message, "fixture-only-secret") {
				t.Fatalf("failure message = %q", message)
			}
		})
	}
}
