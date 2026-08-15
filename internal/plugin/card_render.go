package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	// 宿主 render 输入上限为 1MB。头像 data URL 必须明显低于该值，否则真实微博预览会在
	// 内联 avatar_hd 后触发 platform.render_input_too_large，插件侧只能看到 render.image failed。
	maxPluginRenderDataBytes = 768 << 10
)

func renderSubscriptionCardImage(ctx context.Context, actions pluginActions, template string, data map[string]any, fallback string, extra map[string]any) (string, error) {
	fitPluginRenderData(data)
	request := rayleabot.RenderImageRequest{
		Template: template, Data: data, Theme: "default", Output: "png", FallbackText: fallback,
	}
	result, err := actions.RenderImage(ctx, request)
	imagePath := stringScalar(result["image_path"])
	if err != nil && cardRenderRetryable(ctx, err) {
		result, err = actions.RenderImage(ctx, request)
		imagePath = stringScalar(result["image_path"])
	}
	if err != nil || imagePath == "" {
		logCardRenderFailure(ctx, actions, "订阅卡片图片生成失败", template, data, err, result, extra)
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
	clearAvatarIfDataURL(mapValue(data["author"]), "avatar")
	if original := mapValue(data["original"]); original != nil {
		clearAvatarIfDataURL(mapValue(original["author"]), "avatar")
	}
	for _, card := range mapSliceValue(data["subscriber_cards"]) {
		clearAvatarIfDataURL(card, "avatar_url")
	}
}

func clearAvatarIfDataURL(object map[string]any, key string) {
	if object == nil {
		return
	}
	if strings.HasPrefix(stringScalar(object[key]), "data:") {
		object[key] = ""
	}
}

func logCardRenderFailure(ctx context.Context, actions pluginActions, message, template string, data map[string]any, err error, result rayleabot.ActionResult, extra map[string]any) {
	if actions == nil {
		return
	}
	fields := map[string]any{"template": strings.TrimSpace(template)}
	if encoded, marshalErr := json.Marshal(data); marshalErr == nil {
		fields["data_bytes"] = len(encoded)
	}
	if imagePath := stringScalar(result["image_path"]); imagePath != "" {
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
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func actionErrorLogFields(err error) map[string]any {
	fields := map[string]any{}
	if err == nil {
		return fields
	}
	fields["error"] = diagnosticExcerpt(err.Error(), 500)
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		if actionErr.Code != "" {
			fields["error_code"] = actionErr.Code
		}
		if actionErr.Message != "" {
			fields["error_message"] = diagnosticExcerpt(actionErr.Message, 240)
		}
	}
	return fields
}

func previewCardFailureText(err error) string {
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
		case "plugin.capability_violation":
			return "卡片模板不可用"
		case "platform.render_timeout":
			return "渲染超时"
		case "platform.render_queue_full":
			return "渲染队列繁忙"
		case "platform.render_input_too_large":
			return "渲染数据过大"
		case "platform.invalid_request":
			if reason := diagnosticExcerpt(actionErr.Message, 80); reason != "" && reason != "render.image failed" {
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
			return diagnosticExcerpt(actionErr.Message, 80)
		}
	}
	if err.Error() == "渲染结果没有图片" {
		return "渲染结果没有图片"
	}
	return "图片渲染失败"
}

func previewRenderLogFields(update map[string]any) map[string]any {
	author := mapValue(update["author"])
	return map[string]any{
		"update_id": firstText(update["id"]),
		"uid":       firstText(author["uid"], update["uid"]),
		"platform":  firstText(update["platform"]),
	}
}
