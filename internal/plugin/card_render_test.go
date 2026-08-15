package plugin

import (
	"context"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestRenderSubscriptionCardImageLogsFailureReason(t *testing.T) {
	fake := newFakePluginActions()
	renderErr := &rayleabot.ActionError{Code: "plugin.internal_error", Message: "render.image failed"}
	fake.renderErrors = []error{renderErr, renderErr}
	data := map[string]any{"title": "测试微博", "platform": "微博"}
	_, err := renderSubscriptionCardImage(context.Background(), fake, "weibo-update", data, "回退文案", map[string]any{
		"update_id": "5331543666721397",
	})
	if err == nil {
		t.Fatal("expected render failure")
	}
	if got := previewCardFailureText(err); got != "订阅卡片预览生成失败：图片渲染失败。" {
		t.Fatalf("previewCardFailureText() = %q", got)
	}
	if len(fake.logs) != 1 || fake.logs[0].Message != "订阅卡片图片生成失败" {
		t.Fatalf("render failure was not logged: %#v", fake.logs)
	}
	if len(fake.renders) != 2 {
		t.Fatalf("retryable render attempts = %d, want 2", len(fake.renders))
	}
	fields := fake.logs[0].Fields
	if stringScalar(fields["template"]) != "weibo-update" || stringScalar(fields["error_code"]) != "plugin.internal_error" {
		t.Fatalf("render failure log fields = %#v", fields)
	}
	if stringScalar(fields["update_id"]) != "5331543666721397" || intScalar(fields["data_bytes"]) <= 0 {
		t.Fatalf("render failure log missing diagnostics: %#v", fields)
	}
}

func TestRenderSubscriptionCardImageRetriesTransientHostFailure(t *testing.T) {
	fake := newFakePluginActions()
	fake.renderErrors = []error{&rayleabot.ActionError{Code: "plugin.internal_error", Message: "render.image failed"}}
	data := map[string]any{"title": "测试微博", "platform": "微博"}

	imagePath, err := renderSubscriptionCardImage(context.Background(), fake, "weibo-update", data, "回退文案", nil)
	if err != nil || imagePath != "plugin-test.png" {
		t.Fatalf("renderSubscriptionCardImage() = %q, %v", imagePath, err)
	}
	if len(fake.renders) != 2 {
		t.Fatalf("render attempts = %d, want 2", len(fake.renders))
	}
	if len(fake.logs) != 0 {
		t.Fatalf("successful retry should not log final failure: %#v", fake.logs)
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
	if stringScalar(nestedValue(data, "author", "avatar")) != "" {
		t.Fatalf("oversized author avatar was kept: %#v", data["author"])
	}
	cards := mapSliceValue(data["subscriber_cards"])
	if len(cards) != 1 || stringScalar(cards[0]["avatar_url"]) != "" {
		t.Fatalf("oversized subscriber avatar was kept: %#v", data["subscriber_cards"])
	}
}

func TestCardRenderFailureReasonMapsHostErrors(t *testing.T) {
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_input_too_large", Message: "render input exceeds the configured size limit"}); got != "渲染数据过大" {
		t.Fatalf("too large reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "plugin.capability_violation", Message: "plugin render template belongs to another plugin"}); got != "卡片模板不可用" {
		t.Fatalf("capability reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_timeout", Message: "render execution timed out"}); got != "渲染超时" {
		t.Fatalf("timeout reason = %q", got)
	}
	if got := cardRenderFailureReason(&rayleabot.ActionError{Code: "platform.render_queue_full", Message: "render queue is full"}); got != "渲染队列繁忙" {
		t.Fatalf("queue reason = %q", got)
	}
	if got := previewCardFailureText(nil); got != "订阅卡片预览生成失败。" {
		t.Fatalf("empty preview text = %q", got)
	}
}
