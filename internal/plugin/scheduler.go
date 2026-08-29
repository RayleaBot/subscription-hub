package plugin

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const schedulerTaskID = "subscription-hub-check"
const schedulerCron = "*/1 * * * *"

func (handler *Handler) ensureScheduler(ctx context.Context, event *rayleabot.EventContext) bool {
	if handler.schedulerRegistered.Load() {
		return true
	}
	_, err := handler.hostActions(event).SchedulerCreate(ctx, rayleabot.SchedulerCreateRequest{
		TaskID: schedulerTaskID, Cron: schedulerCron, EventType: "scheduler.trigger",
		LogLabel: "订阅检查", Payload: map[string]any{"action": "check_subscriptions"},
	})
	if err != nil {
		_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
			Level: "warn", Message: "订阅检查定时任务注册失败；自动检查不会按计划运行，请修复后重启插件。原因：" + err.Error(), Fields: map[string]any{"error": err.Error(), "task_id": schedulerTaskID, "cron": schedulerCron},
		})
		return false
	}
	handler.schedulerRegistered.Store(true)
	_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "info", Message: fmt.Sprintf("订阅检查定时任务已注册，计划 %s 运行；插件将按计划检查并推送更新。", schedulerCron),
		Fields: map[string]any{"task_id": schedulerTaskID, "cron": schedulerCron, "log_label": "订阅检查"},
	})
	return true
}

func schedulerJitterDelay() time.Duration {
	return time.Duration(rand.IntN(21)) * time.Second
}
