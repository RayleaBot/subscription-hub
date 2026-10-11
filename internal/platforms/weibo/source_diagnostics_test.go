package weibo

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestWeiboFailureDiagnosticsKeepTransportCauseWithoutCredentialURL(t *testing.T) {
	actions := testkit.NewActions()
	source := newWeiboSource(actions)
	failure := &url.Error{Op: "Get", URL: "https://user:fixture-secret@m.weibo.cn/feed?SUB=fixture-cookie", Err: &net.DNSError{Name: "m.weibo.cn", Err: "fixture dns"}}
	source.recordError(t.Context(), "feed", weiboAccount{}, failure, map[string]any{"duration_ms": 123})
	if len(source.failureDetails) != 1 || len(source.failureKinds) != 1 {
		t.Fatal("failure diagnostics missing")
	}
	detail := source.failureDetails[0]
	if detail["host"] != "m.weibo.cn" || detail["operation"] != "Get" || detail["error_type"] != "*net.DNSError" || detail["duration_ms"] != 123 {
		t.Fatalf("detail=%v", detail)
	}
	encoded, _ := json.Marshal(detail)
	if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "fixture-cookie") {
		t.Fatal("credentials escaped")
	}
}
