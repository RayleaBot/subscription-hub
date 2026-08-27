package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const SubscriptionCheckTimeout = 45 * time.Second
const SubscriptionCheckFinalizationReserve = 5 * time.Second
const SubscriptionCheckIncompleteMessage = "订阅检查未在本轮完成，剩余内容将在后续检查继续处理。"

func newSubscriptionCheckContexts(ctx context.Context) (context.Context, context.Context, context.CancelFunc) {
	now := time.Now()
	stateDeadline := now.Add(SubscriptionCheckTimeout + SubscriptionCheckFinalizationReserve)
	if parentDeadline, exists := ctx.Deadline(); exists && parentDeadline.Before(stateDeadline) {
		stateDeadline = parentDeadline
	}
	stateCtx, cancelState := context.WithDeadline(ctx, stateDeadline)
	checkDeadline := now.Add(SubscriptionCheckTimeout)
	if reservedDeadline := stateDeadline.Add(-SubscriptionCheckFinalizationReserve); reservedDeadline.Before(checkDeadline) {
		checkDeadline = reservedDeadline
	}
	checkCtx, cancelCheck := context.WithDeadline(stateCtx, checkDeadline)
	return checkCtx, stateCtx, func() {
		cancelCheck()
		cancelState()
	}
}

func staleSubscriptionUpdate(update map[string]any, now time.Time, deliveryMaxAge time.Duration) bool {
	if StringScalar(update["service"]) == "live" {
		return false
	}
	pubTS := IntScalar(update["pub_ts"])
	if pubTS <= 0 {
		return false
	}
	publishedAt := time.Unix(int64(pubTS), 0)
	if publishedAt.After(now) {
		return false
	}
	return now.Sub(publishedAt) > deliveryMaxAge
}

func logSubscriptionFailure(ctx context.Context, actions HostActions, message string, item Subscription, err error) {
	fields := map[string]any{"platform": item.Platform, "stage": "send", "subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID}
	for key, value := range actionErrorLogFields(err) {
		fields[key] = value
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func rememberSubscriptionCheck(ctx context.Context, actions HostActions, result map[string]any, now time.Time) error {
	errorsList := stringSlice(result["errors"])
	if len(errorsList) > 3 {
		errorsList = errorsList[:3]
	}
	_, err := actions.KVSet(ctx, "source:subscription:last_result", map[string]any{
		"checked_at": now.UTC().Format(time.RFC3339), "checked": IntScalar(result["checked"]),
		"sent": IntScalar(result["sent"]), "degraded": BoolScalar(result["degraded"]),
		"errors": errorsList, "source": result["source"],
	})
	return err
}

func logSubscriptionCheck(ctx context.Context, actions HostActions, result map[string]any) {
	failures := stringSlice(result["errors"])
	fields := map[string]any{"checked": IntScalar(result["checked"]), "sent": IntScalar(result["sent"])}
	if len(failures) > 0 {
		if len(failures) > 3 {
			failures = failures[:3]
		}
		fields["errors"] = failures
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "订阅源检查降级", Fields: fields})
		return
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: "订阅源检查完成", Fields: fields})
}

func subscriptionCheckSummary(result map[string]any) string {
	switch StringScalar(result["skipped"]) {
	case "disabled":
		return "订阅中心未启用。"
	case "no_checkable_subscriptions", "no_bilibili_subscriptions":
		return "没有可检查的订阅。"
	}
	line := fmt.Sprintf("订阅检查完成：检查 %d 个订阅账号，推送 %d 条更新。", IntScalar(result["checked"]), IntScalar(result["sent"]))
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
	for _, item := range SliceValue(value) {
		if text := StringScalar(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
