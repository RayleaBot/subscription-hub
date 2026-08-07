package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func checkSubscriptions(ctx context.Context, event *rayleabot.EventContext, current settings) map[string]any {
	return checkSubscriptionsWithActions(ctx, event.Actions(), current)
}

func checkSubscriptionsWithActions(ctx context.Context, actions pluginActions, current settings) map[string]any {
	result := map[string]any{
		"handled": true, "checked": 0, "sent": 0, "errors": []string{}, "degraded": false,
	}
	if !current.Enabled {
		result["skipped"] = "disabled"
		return result
	}
	subscriptions := make([]subscription, 0)
	for _, item := range current.Subscriptions {
		if item.Enabled && item.Platform == "bilibili" {
			subscriptions = append(subscriptions, item)
		}
	}
	if len(subscriptions) == 0 {
		result["skipped"] = "no_bilibili_subscriptions"
		return result
	}
	sourceResult := newBilibiliSource(actions).poll(ctx, subscriptions)
	result["checked"] = sourceResult.Checked
	result["source"] = map[string]any{
		"accounts": sourceResult.AccountCount, "dynamic_ok": sourceResult.DynamicOK, "live_ok": sourceResult.LiveOK,
	}
	failures := append([]string(nil), sourceResult.Errors...)
	sent := 0
	preparedCache := map[string]map[string]any{}
	for _, item := range subscriptions {
		dynamicInitialized := dynamicSourceInitialized(ctx, actions, item)
		for _, update := range sourceResult.Updates {
			if !subscriptionMatchesUpdate(item, update) {
				continue
			}
			if stringScalar(update["service"]) != "live" && !dynamicInitialized {
				markUpdateSeen(ctx, actions, item, update)
				continue
			}
			if updateSeen(ctx, actions, item, update) {
				continue
			}
			prepared := prepareBilibiliUpdate(ctx, actions, update, preparedCache)
			if sendBilibiliUpdate(ctx, actions, item, update, prepared, &failures) {
				sent++
			}
		}
		if sourceResult.DynamicOK && subscriptionUsesDynamic(item) && !dynamicInitialized {
			_, _ = actions.KVSet(ctx, dynamicSourceKey(item), true)
		}
	}
	failures = dedupeStrings(failures)
	result["sent"] = sent
	result["errors"] = failures
	result["degraded"] = len(failures) > 0
	rememberSubscriptionCheck(ctx, actions, result)
	return result
}

func subscriptionMatchesUpdate(item subscription, update map[string]any) bool {
	return item.Platform == "bilibili" && item.UID == stringScalar(update["uid"]) && serviceEnabled(item, stringScalar(update["service"]))
}

func subscriptionUsesDynamic(item subscription) bool {
	for _, service := range normalizeServices(item.Services, "bilibili") {
		if service == "all" || service != "live" {
			return true
		}
	}
	return false
}

func dynamicSourceKey(item subscription) string {
	return "source:bilibili:dynamic:initialized:" + item.ID
}

func dynamicSourceInitialized(ctx context.Context, actions pluginActions, item subscription) bool {
	if !subscriptionUsesDynamic(item) {
		return true
	}
	result, _ := actions.KVGet(ctx, dynamicSourceKey(item))
	value, exists := actionStoredValue(result)
	return exists && boolScalar(value)
}

func updateSeen(ctx context.Context, actions pluginActions, item subscription, update map[string]any) bool {
	result, _ := actions.KVGet(ctx, subscriptionUpdateKey(item, update))
	value, exists := actionStoredValue(result)
	return exists && boolScalar(value)
}

func markUpdateSeen(ctx context.Context, actions pluginActions, item subscription, update map[string]any) {
	_, _ = actions.KVSet(ctx, subscriptionUpdateKey(item, update), true)
}

func prepareBilibiliUpdate(ctx context.Context, actions pluginActions, update map[string]any, cache map[string]map[string]any) map[string]any {
	service := stringScalar(update["service"])
	updateID := stringScalar(update["id"])
	cacheKey := service + ":" + updateID
	if cached := cache[cacheKey]; cached != nil {
		return cloneJSONMap(cached)
	}
	prepared := cloneJSONMap(update)
	ref := parseBilibiliPreviewURL(stringScalar(prepared["url"]))
	needsImageTextDetail := service == "image_text" && stringScalar(prepared["summary"]) == "" && stringScalar(prepared["summary_html"]) == ""
	needsOriginal := service == "repost" && mapValue(prepared["original"]) == nil
	if ref != nil && (needsImageTextDetail || needsOriginal) && (ref.Kind == "opus" || ref.Kind == "dynamic") {
		if detailed, err := fetchBilibiliPreview(ctx, actions, ref); err == nil {
			if needsImageTextDetail {
				mergeMissingBilibiliDetail(prepared, detailed)
			}
			if needsOriginal {
				if original := mapValue(detailed["original"]); original != nil {
					prepared["original"] = original
				}
				for _, key := range []string{"summary", "summary_html"} {
					if stringScalar(prepared[key]) == "" && stringScalar(detailed[key]) != "" {
						prepared[key] = detailed[key]
					}
				}
			}
		}
	}
	cache[cacheKey] = cloneJSONMap(prepared)
	return prepared
}

func mergeMissingBilibiliDetail(target, source map[string]any) {
	for _, key := range []string{"summary", "summary_html"} {
		if stringScalar(target[key]) == "" && stringScalar(source[key]) != "" {
			target[key] = source[key]
		}
	}
	if mapValue(target["topic"]) == nil && mapValue(source["topic"]) != nil {
		target["topic"] = source["topic"]
	}
	if len(imageMaps(target["images"], 9)) == 0 && len(imageMaps(source["images"], 9)) > 0 {
		target["images"] = source["images"]
	}
}

func sendBilibiliUpdate(ctx context.Context, actions pluginActions, item subscription, update, prepared map[string]any, failures *[]string) bool {
	data := buildBilibiliRenderData(item, prepared)
	rendered, err := actions.RenderImage(ctx, rayleabot.RenderImageRequest{
		Template: "bilibili-update", Data: data, Theme: "default", Output: "png", FallbackText: buildBilibiliFallback(data),
	})
	if err != nil || stringScalar(rendered["image_path"]) == "" {
		*failures = append(*failures, "Bilibili 订阅图片生成失败。")
		logSubscriptionFailure(ctx, actions, "Bilibili 订阅图片生成失败", item, err)
		return false
	}
	imagePath := stringScalar(rendered["image_path"])
	_, err = actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: item.TargetType, TargetID: item.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	})
	if err != nil {
		*failures = append(*failures, "Bilibili 订阅推送失败。")
		logSubscriptionFailure(ctx, actions, "Bilibili 订阅推送失败", item, err)
		return false
	}
	markUpdateSeen(ctx, actions, item, update)
	return true
}

func logSubscriptionFailure(ctx context.Context, actions pluginActions, message string, item subscription, err error) {
	fields := map[string]any{"subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID}
	if err != nil {
		fields["error"] = diagnosticExcerpt(err.Error(), 500)
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func rememberSubscriptionCheck(ctx context.Context, actions pluginActions, result map[string]any) {
	errorsList := stringSlice(result["errors"])
	if len(errorsList) > 3 {
		errorsList = errorsList[:3]
	}
	_, _ = actions.KVSet(ctx, "source:bilibili:last_result", map[string]any{
		"checked_at": time.Now().UTC().Format(time.RFC3339), "checked": intScalar(result["checked"]),
		"sent": intScalar(result["sent"]), "degraded": boolScalar(result["degraded"]),
		"errors": errorsList, "source": result["source"],
	})
}

func logSubscriptionCheck(ctx context.Context, actions pluginActions, result map[string]any) {
	failures := stringSlice(result["errors"])
	fields := map[string]any{"checked": intScalar(result["checked"]), "sent": intScalar(result["sent"])}
	if len(failures) > 0 {
		if len(failures) > 3 {
			failures = failures[:3]
		}
		fields["errors"] = failures
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "Bilibili 订阅源检查降级", Fields: fields})
		return
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: "Bilibili 订阅源检查完成", Fields: fields})
}

func subscriptionCheckSummary(result map[string]any) string {
	switch stringScalar(result["skipped"]) {
	case "disabled":
		return "订阅中心未启用。"
	case "no_bilibili_subscriptions":
		return "没有可检查的 Bilibili 订阅。"
	}
	line := fmt.Sprintf("订阅检查完成：检查 %d 个订阅账号，推送 %d 条更新。", intScalar(result["checked"]), intScalar(result["sent"]))
	failures := stringSlice(result["errors"])
	if len(failures) == 0 {
		return line
	}
	lines := []string{line}
	for index, failure := range failures {
		if index == 3 {
			break
		}
		lines = append(lines, "- "+failure)
	}
	return strings.Join(lines, "\n")
}

func stringSlice(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	result := make([]string, 0)
	for _, item := range sliceValue(value) {
		if text := stringScalar(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
