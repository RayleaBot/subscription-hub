package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const CardRenderActionTimeout = 35 * time.Second

const maxPluginRenderRequestBytes = 1 << 20

const pluginRenderDataHeadroomBytes = 64 << 10

// Reserve room for host metadata within the 1 MiB render request limit.
const maxPluginRenderDataBytes = maxPluginRenderRequestBytes - pluginRenderDataHeadroomBytes

func RenderSubscriptionCardImage(ctx context.Context, actions HostActions, template string, data map[string]any, fallback string, extra map[string]any) (string, error) {
	return RenderSubscriptionCardImageWithResources(ctx, actions, template, data, nil, fallback, extra)
}

type RenderResource struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	FallbackURLs []string `json:"fallback_urls,omitempty"`
	Referer      string   `json:"referer,omitempty"`
}

type PluginRenderImageRequest struct {
	Template     string           `json:"template"`
	Data         map[string]any   `json:"data"`
	Theme        string           `json:"theme,omitempty"`
	Output       string           `json:"output,omitempty"`
	FallbackText string           `json:"fallback_text,omitempty"`
	Resources    []RenderResource `json:"resources,omitempty"`
}

func RenderSubscriptionCardImageWithResources(ctx context.Context, actions HostActions, template string, data map[string]any, resources []RenderResource, fallback string, extra map[string]any) (string, error) {
	fitPluginRenderData(data)
	render := func() (rayleabot.ActionResult, error) {
		if len(resources) == 0 {
			return actions.RenderImage(ctx, rayleabot.RenderImageRequest{
				Template: template, Data: data, Theme: "default", Output: "png", FallbackText: fallback,
			})
		}
		caller, ok := actions.(GenericLocalActionCaller)
		if !ok {
			return nil, errors.New("渲染资源调用不可用")
		}
		request := PluginRenderImageRequest{
			Template: template, Data: data, Theme: "default", Output: "png", FallbackText: fallback, Resources: resources,
		}
		result := rayleabot.ActionResult{}
		if err := caller.Call(ctx, "render.image", request, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
	result, err := render()
	imagePath := StringScalar(result["image_path"])
	if err != nil && cardRenderRetryable(ctx, err) {
		result, err = render()
		imagePath = StringScalar(result["image_path"])
	}
	if err != nil || imagePath == "" {
		logCardRenderFailure(ctx, actions, template, data, err, result, extra)
		if err != nil {
			return "", err
		}
		return "", errors.New("渲染结果没有图片")
	}
	return imagePath, nil
}

func cardRenderRetryable(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var actionErr *rayleabot.ActionError
	if !errors.As(err, &actionErr) {
		return false
	}
	return actionErr.Code == "plugin.internal_error" || actionErr.Code == "platform.internal_error"
}

func fitPluginRenderData(data map[string]any) {
	if data == nil {
		return
	}
	encoded, err := json.Marshal(data)
	if err != nil || len(encoded) <= maxPluginRenderDataBytes {
		return
	}
	clearRenderDataURLs(data)
}

func clearRenderDataURLs(data map[string]any) {
	clearAvatarIfDataURL(MapValue(data["author"]), "avatar")
	if original := MapValue(data["original"]); original != nil {
		clearAvatarIfDataURL(MapValue(original["author"]), "avatar")
	}
	for _, card := range MapSliceValue(data["subscriber_cards"]) {
		clearAvatarIfDataURL(card, "avatar_url")
	}
}

func clearAvatarIfDataURL(object map[string]any, key string) {
	if object == nil {
		return
	}
	if strings.HasPrefix(StringScalar(object[key]), "data:") {
		object[key] = ""
	}
}

func logCardRenderFailure(ctx context.Context, actions HostActions, template string, data map[string]any, err error, result rayleabot.ActionResult, extra map[string]any) {
	if actions == nil {
		return
	}
	fields := map[string]any{"template": strings.TrimSpace(template)}
	if encoded, marshalErr := json.Marshal(data); marshalErr == nil {
		fields["data_bytes"] = len(encoded)
	}
	if imagePath := StringScalar(result["image_path"]); imagePath != "" {
		fields["has_image_path"] = true
	} else {
		fields["has_image_path"] = false
	}
	for key, value := range actionErrorLogFields(err) {
		fields[key] = value
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		fields[key] = value
	}
	reason := cardRenderFailureReason(err)
	if reason == "" {
		reason = "渲染结果没有图片路径"
	}
	message := fmt.Sprintf("订阅卡片生成失败（%s）：%s。", strings.TrimSpace(template), reason)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func actionErrorLogFields(err error) map[string]any {
	fields := map[string]any{}
	if err == nil {
		return fields
	}
	fields["error"] = DiagnosticExcerpt(err.Error(), 500)
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		if actionErr.Code != "" {
			fields["error_code"] = actionErr.Code
		}
		if actionErr.Message != "" {
			fields["error_message"] = DiagnosticExcerpt(actionErr.Message, 240)
		}
	}
	return fields
}

func PreviewCardFailureText(err error) string {
	reason := cardRenderFailureReason(err)
	if reason == "" {
		return "订阅卡片预览生成失败。"
	}
	return "订阅卡片预览生成失败：" + reason + "。"
}

func cardRenderFailureReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "deadline") {
		return "渲染超时"
	}
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		switch actionErr.Code {
		case "plugin.permission_denied":
			return "卡片模板不可用"
		case "platform.render_timeout":
			return "渲染超时"
		case "platform.render_queue_full":
			return "渲染队列繁忙"
		case "platform.render_input_too_large":
			return "渲染数据过大"
		case "platform.invalid_request":
			if reason := DiagnosticExcerpt(actionErr.Message, 80); reason != "" && reason != "render.image failed" {
				return reason
			}
			return "卡片数据无效"
		}
		message := strings.ToLower(actionErr.Message + " " + actionErr.Code)
		if strings.Contains(message, "too large") || strings.Contains(actionErr.Message, "过大") {
			return "渲染数据过大"
		}
		if strings.Contains(message, "timeout") {
			return "渲染超时"
		}
		if strings.TrimSpace(actionErr.Message) != "" && actionErr.Message != "render.image failed" && actionErr.Message != "local action failed" {
			return DiagnosticExcerpt(actionErr.Message, 80)
		}
	}
	if err.Error() == "渲染结果没有图片" {
		return "渲染结果没有图片"
	}
	return "图片渲染失败"
}

func PreviewRenderLogFields(update map[string]any) map[string]any {
	author := MapValue(update["author"])
	return map[string]any{
		"update_id": FirstText(update["id"]),
		"uid":       FirstText(author["uid"], update["uid"]),
		"platform":  FirstText(update["platform"]),
	}
}
