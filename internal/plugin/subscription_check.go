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
	reason := "未知错误"
	if err != nil {
		reason = DiagnosticExcerpt(err.Error(), 240)
	}
	completeMessage := fmt.Sprintf("%s：平台 %s，订阅 %s，目标 %s %s；本条更新未送达。原因：%s", message, item.Platform, item.ID, item.TargetType, item.TargetID, reason)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: completeMessage, Fields: fields})
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
	checked, sent := IntScalar(result["checked"]), IntScalar(result["sent"])
	fields := map[string]any{"checked": checked, "sent": sent, "failure_count": len(failures)}
	if len(failures) > 0 {
		fields["errors"] = failures
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: subscriptionCheckLogMessage(checked, sent, failures), Fields: fields})
		return
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: fmt.Sprintf("订阅检查完成：检查 %d 个订阅源，推送 %d 条更新；未发现异常。", checked, sent), Fields: fields})
}

func subscriptionCheckLogMessage(checked, sent int64, failures []string) string {
	visible := failures
	if len(visible) > 3 {
		visible = visible[:3]
	}
	reasons := make([]string, 0, len(visible))
	for _, failure := range visible {
		if text := strings.TrimSpace(strings.TrimRight(failure, "。；")); text != "" {
			reasons = append(reasons, text)
		}
	}
	message := fmt.Sprintf("订阅检查降级：检查 %d 个订阅源，推送 %d 条更新；发现 %d 类异常", checked, sent, len(failures))
	if len(reasons) > 0 {
		message += "：" + strings.Join(reasons, "；")
	}
	if remaining := len(failures) - len(visible); remaining > 0 {
		message += fmt.Sprintf("；另有 %d 类异常保留在日志详情中", remaining)
	}
	return message + "。"
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
