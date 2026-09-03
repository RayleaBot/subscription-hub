package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestRenderSubscriptionCardImageLogsFailureReason(t *testing.T) {
	fake := testkit.NewActions()
	renderErr := &rayleabot.ActionError{Code: "plugin.internal_error", Message: "render.image failed"}
	fake.RenderErrors = []error{renderErr, renderErr}
	data := map[string]any{"title": "测试微博", "platform": "微博"}
	_, err := RenderSubscriptionCardImage(context.Background(), fake, "weibo-update", data, "回退文案", map[string]any{
		"update_id": "5331543666721397",
	})
	if err == nil {
		t.Fatal("expected render failure")
	}
	if got := PreviewCardFailureText(err); got != "订阅卡片预览生成失败：图片渲染失败。" {
		t.Fatalf("previewCardFailureText() = %q", got)
	}
	if len(fake.Logs) != 1 || !strings.Contains(fake.Logs[0].Message, "weibo-update") || !strings.Contains(fake.Logs[0].Message, "本次卡片不会发送") {
		t.Fatalf("render failure was not logged: %#v", fake.Logs)
	}
	if len(fake.Renders) != 2 {
		t.Fatalf("retryable render attempts = %d, want 2", len(fake.Renders))
	}
	fields := fake.Logs[0].Fields
	if StringScalar(fields["template"]) != "weibo-update" || StringScalar(fields["error_code"]) != "plugin.internal_error" {
		t.Fatalf("render failure log fields = %#v", fields)
	}
	if StringScalar(fields["update_id"]) != "5331543666721397" || IntScalar(fields["data_bytes"]) <= 0 {
		t.Fatalf("render failure log missing diagnostics: %#v", fields)
	}
}

func TestRenderSubscriptionCardImageRetriesTransientHostFailure(t *testing.T) {
	fake := testkit.NewActions()
	fake.RenderErrors = []error{&rayleabot.ActionError{Code: "plugin.internal_error", Message: "render.image failed"}}
	data := map[string]any{"title": "测试微博", "platform": "微博"}

	imagePath, err := RenderSubscriptionCardImage(context.Background(), fake, "weibo-update", data, "回退文案", nil)
	if err != nil || imagePath != "plugin-test.png" {
		t.Fatalf("renderSubscriptionCardImage() = %q, %v", imagePath, err)
	}
	if len(fake.Renders) != 2 {
		t.Fatalf("render attempts = %d, want 2", len(fake.Renders))
	}
	if len(fake.Logs) != 0 {
		t.Fatalf("successful retry should not log final failure: %#v", fake.Logs)
	}
}

func TestFitPluginRenderDataClearsOversizedAvatars(t *testing.T) {
	data := map[string]any{
		"title":  "测试微博",
		"author": map[string]any{"avatar": "data:image/jpeg;base64," + strings.Repeat("A", maxPluginRenderDataBytes)},
		"subscriber_cards": []map[string]any{
			{"avatar_url": "data:image/png;base64,fixture"},
		},
	}
	fitPluginRenderData(data)
	if StringScalar(NestedValue(data, "author", "avatar")) != "" {
		t.Fatalf("oversized author avatar was kept: %#v", data["author"])
	}
	cards := MapSliceValue(data["subscriber_cards"])
	if len(cards) != 1 || StringScalar(cards[0]["avatar_url"]) != "" {
		t.Fatalf("oversized subscriber avatar was kept: %#v", data["subscriber_cards"])
	}
}

func TestCardRenderFailureReasonMapsHostErrors(t *testing.T) {
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_input_too_large", Message: "render input exceeds the configured size limit"}); got != "渲染数据过大" {
		t.Fatalf("too large reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "plugin.permission_denied", Message: "plugin render template belongs to another plugin"}); got != "卡片模板不可用" {
		t.Fatalf("permission reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_timeout", Message: "render execution timed out"}); got != "渲染超时" {
		t.Fatalf("timeout reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_queue_full", Message: "render queue is full"}); got != "渲染队列繁忙" {
		t.Fatalf("queue reason = %q", got)
	}
	if got := PreviewCardFailureText(nil); got != "订阅卡片预览生成失败。" {
		t.Fatalf("empty preview text = %q", got)
	}
}

func TestRenderReplyCardDoesNotRetryRemoteData(t *testing.T) {
	primary := map[string]any{"avatar": "data:image/webp;base64,fixture"}
	fake := testkit.NewActions()
	fake.RenderErrors = []error{errors.New("render input too large"), nil}

	imagePath, err := newWorkflowHandler(t).renderReplyCard(context.Background(), fake, CardRequest{Template: "fixture-search", Data: primary, Fallback: "fallback text"}, nil)
	if err == nil || imagePath != "" {
		t.Fatalf("render failure was hidden: path=%q err=%v", imagePath, err)
	}
	if len(fake.Renders) != 1 || StringScalar(fake.Renders[0].Data["avatar"]) != StringScalar(primary["avatar"]) {
		t.Fatalf("unexpected render requests: %#v", fake.Renders)
	}
}

func TestRenderReplyCardWaitsPastServerRenderTimeout(t *testing.T) {
	actions := &deadlineRenderActions{Actions: testkit.NewActions()}
	started := time.Now()
	imagePath, err := newWorkflowHandler(t).renderReplyCard(context.Background(), actions, CardRequest{Template: "fixture-search", Data: map[string]any{}, Fallback: "fallback text"}, nil)
	if err != nil || imagePath != "plugin-test.png" {
		t.Fatalf("render failed: path=%q err=%v", imagePath, err)
	}
	wait := actions.deadline.Sub(started)
	if wait <= 30*time.Second || wait > CardRenderActionTimeout+time.Second {
		t.Fatalf("render action timeout = %s, want > 30s and <= %s", wait, CardRenderActionTimeout)
	}
}

type deadlineRenderActions struct {
	*testkit.Actions
	deadline time.Time
}

func (actions *deadlineRenderActions) RenderImage(ctx context.Context, request rayleabot.RenderImageRequest) (rayleabot.ActionResult, error) {
	actions.deadline, _ = ctx.Deadline()
	return actions.Actions.RenderImage(ctx, request)
}
