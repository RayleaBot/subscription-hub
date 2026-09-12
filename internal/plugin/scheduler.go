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
const deferredMediaTaskID = "subscription-hub-media"
const deferredMediaCron = "*/1 * * * *"
const accountCheckTaskID = "subscription-hub-accounts"
const accountCheckCron = "*/15 * * * *"

func (handler *Handler) ensureScheduler(ctx context.Context, event *rayleabot.EventContext) bool {
	if handler.schedulerRegistered.Load() {
		return true
	}
	select {
	case handler.schedulerGate <- struct{}{}:
		defer func() { <-handler.schedulerGate }()
	case <-ctx.Done():
		return false
	}
	if handler.schedulerRegistered.Load() {
		return true
	}
	tasks := []rayleabot.SchedulerCreateRequest{
		{TaskID: schedulerTaskID, Cron: schedulerCron, EventType: "scheduler.trigger", LogLabel: "订阅检查", Payload: map[string]any{"action": "check_subscriptions"}},
		{TaskID: deferredMediaTaskID, Cron: deferredMediaCron, EventType: "scheduler.trigger", LogLabel: "解析媒体发送", Payload: map[string]any{"action": "flush_deferred_media"}},
		{TaskID: accountCheckTaskID, Cron: accountCheckCron, EventType: "scheduler.trigger", LogLabel: "账号检查", Payload: map[string]any{"action": "check_accounts"}},
	}
	for _, task := range tasks {
		if _, err := handler.hostActions(event).SchedulerCreate(ctx, task); err != nil {
			_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
				Level: "warn", Message: task.LogLabel + "定时任务设置失败，稍后重试：" + err.Error(),
				Fields: map[string]any{"error": err.Error(), "task_id": task.TaskID, "cron": task.Cron},
			})
			return false
		}
	}
	handler.schedulerRegistered.Store(true)
	_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "debug", Message: fmt.Sprintf("已设置 %d 项定时任务。", len(tasks)),
		Fields: map[string]any{"task_id": schedulerTaskID, "media_task_id": deferredMediaTaskID, "cron": schedulerCron},
	})
	return true
}

func schedulerJitterDelay() time.Duration {
	return time.Duration(rand.IntN(21)) * time.Second
}
