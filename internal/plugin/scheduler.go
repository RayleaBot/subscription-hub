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
	}
	for _, task := range tasks {
		if _, err := handler.hostActions(event).SchedulerCreate(ctx, task); err != nil {
			_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
				Level: "warn", Message: task.LogLabel + "定时任务注册未完成；后续事件将重试注册。原因：" + err.Error(),
				Fields: map[string]any{"error": err.Error(), "task_id": task.TaskID, "cron": task.Cron},
			})
			return false
		}
	}
	handler.schedulerRegistered.Store(true)
	_, _ = handler.hostActions(event).LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "info", Message: fmt.Sprintf("订阅检查与解析媒体发送任务已注册，每分钟检查一次，共 %d 项任务。", len(tasks)),
		Fields: map[string]any{"task_id": schedulerTaskID, "media_task_id": deferredMediaTaskID, "cron": schedulerCron},
	})
	return true
}

func schedulerJitterDelay() time.Duration {
	return time.Duration(rand.IntN(21)) * time.Second
}
