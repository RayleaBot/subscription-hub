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
			Level: "warn", Message: "订阅检查任务注册失败", Fields: map[string]any{"error": err.Error()},
		})
		return false
	}
	handler.schedulerRegistered.Store(true)
	_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "info", Message: fmt.Sprintf("订阅中心插件创建定时任务订阅检查（%s）", schedulerCron),
		Fields: map[string]any{"task_id": schedulerTaskID, "cron": schedulerCron, "log_label": "订阅检查"},
	})
	return true
}

func schedulerJitterDelay() time.Duration {
	return time.Duration(rand.IntN(21)) * time.Second
}
