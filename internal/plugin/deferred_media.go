package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	// 后台下载不与任何事件共享预算：大视频可以慢慢下载，再由下一次
	// 定时任务在自己的事件窗口内完成发送。
	deferredMediaDownloadTimeout = 15 * time.Minute
	deferredMediaMaxJobs         = 8
	deferredMediaMaxSendAttempts = 3
	deferredMediaFlushTimeout    = 40 * time.Second
)

type deferredMediaJob struct {
	TargetType    string
	TargetID      string
	Platform      string
	SubjectName   string
	SenderID      string
	SenderName    string
	SourceAdapter string
	Sources       []ResolverMediaSource
	Settings      ResolverMediaSettings

	mu          sync.Mutex
	prepared    []preparedResolverMedia
	tempRoot    string
	failed      string
	attempts    int
	retainUntil time.Time
}

func (job *deferredMediaJob) cleanup() {
	job.mu.Lock()
	tempRoot := job.tempRoot
	job.tempRoot = ""
	job.prepared = nil
	job.mu.Unlock()
	if tempRoot != "" {
		_ = os.RemoveAll(tempRoot)
	}
}

func (job *deferredMediaJob) complete(tempRoot string, prepared []preparedResolverMedia) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.tempRoot = tempRoot
	job.prepared = prepared
}

func (job *deferredMediaJob) markFailed(message string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.failed = message
}

// bumpAttempts 返回是否还能继续重试发送。
func (job *deferredMediaJob) bumpAttempts() bool {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.attempts++
	return job.attempts < deferredMediaMaxSendAttempts
}

// deferredMediaQueue 保存在事件预算内放不下的媒体任务：下载在后台
// 进行，发送由下一次 scheduler.trigger 事件执行。所有读写都加锁。
type deferredMediaQueue struct {
	root    string
	mu      sync.Mutex
	flushMu sync.Mutex
	jobs    []*deferredMediaJob
}

func newDeferredMediaQueue(root string) *deferredMediaQueue {
	if root == "" {
		root = filepath.Join(os.TempDir(), "raylea-subscription-media")
	}
	queue := &deferredMediaQueue{root: root}
	queue.loadRetained()
	return queue
}

func (queue *deferredMediaQueue) push(job *deferredMediaJob) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.pruneRetainedLocked(time.Now())
	if len(queue.jobs) >= deferredMediaMaxJobs {
		return false
	}
	queue.jobs = append(queue.jobs, job)
	return true
}

func (queue *deferredMediaQueue) readyJobs() []*deferredMediaJob {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.pruneRetainedLocked(time.Now())
	ready := make([]*deferredMediaJob, 0, len(queue.jobs))
	for _, job := range queue.jobs {
		job.mu.Lock()
		completed := job.retainUntil.IsZero() && (job.failed != "" || len(job.prepared) > 0)
		job.mu.Unlock()
		if completed {
			ready = append(ready, job)
		}
	}
	return ready
}

func (queue *deferredMediaQueue) remove(job *deferredMediaJob) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for index, candidate := range queue.jobs {
		if candidate == job {
			queue.jobs = append(queue.jobs[:index], queue.jobs[index+1:]...)
			return
		}
	}
}

// deferMediaForBackground 判断媒体是否应该转后台。抖音视频下载不占用
// 消息事件预算；直播必须实时录制，不转后台。
func (handler *Handler) deferMediaForBackground(platform string, plan ResolverMediaPlan) bool {
	if platform != "douyin" {
		return false
	}
	hasVideo := false
	for _, source := range plan.Sources {
		if source.Kind == "live" {
			return false
		}
		if source.Kind == "video" {
			hasVideo = true
		}
	}
	return hasVideo
}

// enqueueDeferredMedia 把媒体任务转入后台下载队列，下载完成后由下一次
// 定时任务发送。队列已满时返回 false，由调用方报告未入队原因。
func (handler *Handler) enqueueDeferredMedia(target resolverSendTarget, platform string, plan ResolverMediaPlan, settings ResolverMediaSettings) bool {
	job := &deferredMediaJob{
		TargetType:  target.TargetType,
		TargetID:    target.TargetID,
		Platform:    platform,
		SubjectName: target.SubjectName, SenderID: target.SenderID, SenderName: target.SenderName,
		SourceAdapter: target.SourceAdapter,
		Sources:       plan.Sources,
		Settings:      settings,
	}
	if !handler.deferredMedia.push(job) {
		return false
	}
	go handler.downloadDeferredMedia(job)
	return true
}

func (handler *Handler) downloadDeferredMedia(job *deferredMediaJob) {
	ctx, cancel := context.WithTimeout(context.Background(), deferredMediaDownloadTimeout)
	defer cancel()
	release, err := handler.mediaGate.acquire(ctx, job.Settings.MediaConcurrency)
	if err != nil {
		job.markFailed("后台下载队列繁忙，请稍后重新分享链接")
		return
	}
	defer release()
	tempRoot, err := handler.deferredMedia.createTemp()
	if err != nil {
		job.markFailed("创建临时目录：" + err.Error())
		return
	}
	prepared, err := prepareResolverMediaSources(ctx, tempRoot, job.Sources, job.Settings)
	if err != nil {
		_ = os.RemoveAll(tempRoot)
		job.markFailed(err.Error())
		return
	}
	job.complete(tempRoot, prepared)
}

// flushDeferredMedia 在 scheduler.trigger 事件内发送已下载完成的媒体任务。
// 限流失败可在后续触发中重试；发送超时因结果不确定，不自动重发。
func (handler *Handler) flushDeferredMedia(ctx context.Context, event *rayleabot.EventContext) {
	if !handler.deferredMedia.flushMu.TryLock() {
		return
	}
	defer handler.deferredMedia.flushMu.Unlock()
	jobs := handler.deferredMedia.readyJobs()
	if len(jobs) == 0 {
		return
	}
	actions := handler.hostActions(event)
	flushCtx, cancel := context.WithTimeout(ctx, deferredMediaFlushTimeout)
	defer cancel()
	for _, job := range jobs {
		if flushCtx.Err() != nil {
			break
		}
		job.mu.Lock()
		failed := job.failed
		prepared := append([]preparedResolverMedia(nil), job.prepared...)
		job.mu.Unlock()
		if failed != "" {
			handler.deferredMedia.remove(job)
			job.cleanup()
			handler.sendDeferredFailure(flushCtx, actions, job, failed)
			continue
		}
		if len(prepared) == 0 {
			continue
		}
		err := sendPreparedResolverMedia(flushCtx, actions, job.Platform, prepared, job.Settings, resolverSendTarget{
			TargetType: job.TargetType, TargetID: job.TargetID,
			SubjectName: job.SubjectName, SenderID: job.SenderID, SenderName: job.SenderName, SourceAdapter: job.SourceAdapter,
		})
		if err != nil {
			// 仅限流可安全重试（动作已被宿主明确拒绝）。超时不重试：
			// 插件放弃等待后宿主可能仍完成发送，重试会造成重复发送。
			var actionErr *rayleabot.ActionError
			var partial *partialMediaSendError
			if !errors.As(err, &partial) && errors.As(err, &actionErr) && actionErr.Code == "platform.rate_limited" && job.bumpAttempts() {
				continue
			}
			if mediaOutcomeUncertain(err) {
				job.retain()
				if ctx.Err() == nil {
					_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "未确认媒体是否送达，不自动重发。", Fields: actionErrorLogFields(err)})
				}
				return
			}
			handler.deferredMedia.remove(job)
			job.cleanup()
			message := "媒体发送失败：" + err.Error()
			handler.sendDeferredFailure(flushCtx, actions, job, message)
			continue
		}
		handler.deferredMedia.remove(job)
		job.cleanup()
	}
}

func (handler *Handler) sendDeferredFailure(ctx context.Context, actions HostActions, job *deferredMediaJob, reason string) {
	if ctx.Err() != nil {
		return
	}
	message := EnsureSentence(reason)
	_, _ = actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		SourceAdapter: job.SourceAdapter,
		TargetType:    job.TargetType, TargetID: job.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(message)}},
	})
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "warn", Message: "媒体发送未完成：" + message,
		Fields: map[string]any{"platform": job.Platform, "target": job.TargetType + ":" + job.TargetID},
	})
}
