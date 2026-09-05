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
	outcome := "本条更新未送达"
	if mediaOutcomeUncertain(err) {
		outcome = "未确认是否送达，不自动重发"
	}
	completeMessage := fmt.Sprintf("%s（%s）；%s：%s", message, FirstText(item.Name, item.UID, item.ID), outcome, reason)
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

func logSubscriptionCheck(ctx context.Context, actions HostActions, result map[string]any, repeats ...int) {
	failures := stringSlice(result["errors"])
	checked, sent := IntScalar(result["checked"]), IntScalar(result["sent"])
	fields := map[string]any{"checked": checked, "sent": sent, "failure_count": len(failures)}
	if causes := stringSlice(result["failure_causes"]); len(causes) > 0 {
		fields["failure_causes"] = causes
	}
	if counts, ok := result["failure_counts"].(map[string]int); ok {
		fields["failure_counts"] = counts
	}
	if len(repeats) > 0 && repeats[0] > 0 {
		fields["repeat_count"] = repeats[0]
	}
	if BoolScalar(result["paused_only"]) {
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "debug", Message: "部分订阅检查暂停，等待重试。", Fields: fields})
		return
	}
	if len(failures) > 0 {
		fields["errors"] = failures
		message := subscriptionCheckLogMessage(checked, sent, failures)
		if platforms := stringSlice(result["failure_platforms"]); len(platforms) > 0 {
			fields["failure_platforms"] = platforms
			message = strings.Join(platforms, "、") + message
		}
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
		return
	}
	level := "debug"
	if sent > 0 {
		level = "info"
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: level, Message: fmt.Sprintf("订阅检查完成：检查 %d 个账号，推送 %d 条更新。", checked, sent), Fields: fields})
}

func (handler *Handler) logCheck(ctx context.Context, actions HostActions, result map[string]any) {
	if ctx.Err() != nil {
		return
	}
	if BoolScalar(result["paused_only"]) {
		logSubscriptionCheck(ctx, actions, result)
		return
	}
	handler.checkLogMu.Lock()
	failures := len(stringSlice(result["errors"]))
	count := 0
	counts := make(map[string]int)
	if failures > 0 {
		if handler.checkLogs == nil {
			handler.checkLogs = make(map[string]checkLogState)
		}
		causes := stringSlice(result["failure_causes"])
		if len(causes) == 0 {
			causes = []string{"subscription:check:unspecified"}
		}
		now := handler.now()
		for _, cause := range DedupeStrings(causes) {
			if _, exists := handler.checkLogs[cause]; !exists && len(handler.checkLogs) >= 1024 {
				var oldest string
				for key, entry := range handler.checkLogs {
					if oldest == "" || entry.emitted.Before(handler.checkLogs[oldest].emitted) {
						oldest = key
					}
				}
				delete(handler.checkLogs, oldest)
			}
			state := handler.checkLogs[cause]
			state.total++
			state.pending++
			if state.emitted.IsZero() || now.Sub(state.emitted) >= 5*time.Minute {
				counts[cause] = state.pending
				count += state.pending
				state.pending, state.emitted = 0, now
			}
			handler.checkLogs[cause] = state
		}
	}
	recovered := failures == 0 && !BoolScalar(result["paused"]) && StringScalar(result["skipped"]) == "" && len(handler.checkLogs) > 0
	if recovered {
		for cause, state := range handler.checkLogs {
			counts[cause] = state.total
			count += state.total
		}
		handler.checkLogs = nil
	}
	handler.checkLogMu.Unlock()
	if failures > 0 && count == 0 {
		return
	}
	if recovered {
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: fmt.Sprintf("订阅检查已恢复：检查 %d 个账号，推送 %d 条更新。", IntScalar(result["checked"]), IntScalar(result["sent"])), Fields: map[string]any{"repeat_count": count, "failure_counts": counts, "checked": IntScalar(result["checked"]), "sent": IntScalar(result["sent"]), "failure_count": 0}})
		return
	}
	logged := make(map[string]any, len(result)+1)
	for key, value := range result {
		logged[key] = value
	}
	logged["failure_counts"] = counts
	var platforms []string
	for _, cause := range stringSlice(result["failure_causes"]) {
		id, _, _ := strings.Cut(cause, ":")
		if platform, ok := handler.byID[id]; ok {
			platforms = append(platforms, FirstText(platform.Name, platform.ID))
		}
	}
	logged["failure_platforms"] = DedupeStrings(platforms)
	logSubscriptionCheck(ctx, actions, logged, count)
}

func subscriptionCheckLogMessage(checked, sent int64, failures []string) string {
	return fmt.Sprintf("订阅检查未全部完成：检查 %d 个账号，推送 %d 条更新，%d 项异常。", checked, sent, len(failures))
}

func subscriptionCheckSummary(result map[string]any) string {
	switch StringScalar(result["skipped"]) {
	case "check_in_progress":
		return "已有订阅检查正在执行，本轮已跳过。"
	case "check_canceled":
		return "等待订阅检查时已取消，本轮未执行。"
	case "disabled":
		return "订阅功能未启用。"
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
