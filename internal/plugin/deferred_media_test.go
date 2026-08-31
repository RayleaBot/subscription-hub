package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestDeferMediaForBackground(t *testing.T) {
	handler := newWorkflowHandler(t)
	videoPlan := ResolverMediaPlan{Sources: []ResolverMediaSource{{Kind: "video", URLs: []string{"https://example.com/v.mp4"}}}}
	// 抖音视频统一转后台：CDN 慢且按 TLS 指纹拒绝 Go 客户端。
	if !handler.deferMediaForBackground("douyin", videoPlan) {
		t.Fatalf("douyin video must defer to background")
	}
	if handler.deferMediaForBackground("bilibili", videoPlan) {
		t.Fatalf("non-douyin media must stay sync")
	}
	// 直播必须实时录制，从不延迟。
	livePlan := ResolverMediaPlan{Sources: []ResolverMediaSource{{Kind: "live", URLs: []string{"https://example.com/live.flv"}}}}
	if handler.deferMediaForBackground("douyin", livePlan) {
		t.Fatalf("live media must never defer")
	}
	// 图文（纯图片）不延迟。
	imagePlan := ResolverMediaPlan{Sources: []ResolverMediaSource{{Kind: "image", URLs: []string{"https://example.com/i.jpg"}}}}
	if handler.deferMediaForBackground("douyin", imagePlan) {
		t.Fatalf("image media must stay sync")
	}
}

func TestDouyinVideoDownloadHosts(t *testing.T) {
	for _, rawURL := range []string{
		"https://aweme.snssdk.com/aweme/v1/play/?video_id=test",
		"https://aweme.snssdk.com/aweme/v1/playwm/?video_id=test",
		"https://v5-gz2-colda.douyinvod.com/path/video.mp4",
		"https://v5-hl-mly-ov.zjcdn.com/path/video.mp4",
	} {
		if !douyinVideoDownloadHosts([]string{rawURL}) {
			t.Fatalf("douyinVideoDownloadHosts(%q) = false, want true", rawURL)
		}
	}
	for _, rawURL := range []string{
		"https://p3-pc.douyinpic.com/aweme/cover.jpeg",
		"https://upos-sz-mirror.bilivideo.com/video.mp4",
		"https://example.com/video.mp4",
	} {
		if douyinVideoDownloadHosts([]string{rawURL}) {
			t.Fatalf("douyinVideoDownloadHosts(%q) = true, want false", rawURL)
		}
	}
}

func TestDeferredMediaDownloadAndFlush(t *testing.T) {
	videoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "12")
		_, _ = w.Write([]byte("fixturevideo"))
	}))
	defer videoServer.Close()

	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.actions = actions
	job := &deferredMediaJob{
		TargetType: "group", TargetID: "12345", Platform: "douyin",
		Sources:  []ResolverMediaSource{{Kind: "video", URLs: []string{videoServer.URL}, FileName: "douyin-test.mp4"}},
		Settings: ResolverMediaSettings{},
	}
	if !handler.deferredMedia.push(job) {
		t.Fatalf("queue push failed")
	}
	handler.downloadDeferredMedia(job)
	job.mu.Lock()
	if job.failed != "" || len(job.prepared) == 0 {
		job.mu.Unlock()
		t.Fatalf("background download failed: %q prepared=%d", job.failed, len(job.prepared))
	}
	job.mu.Unlock()

	handler.flushDeferredMedia(context.Background(), &rayleabot.EventContext{})
	if len(handler.deferredMedia.jobs) != 0 {
		t.Fatalf("flushed job still queued: %#v", handler.deferredMedia.jobs)
	}
	if len(actions.Messages) != 1 {
		t.Fatalf("flush sent %d messages, want 1", len(actions.Messages))
	}
	segments := actions.Messages[0].Message.Segments
	if len(segments) != 1 || segments[0].Type != "video" || !strings.Contains(segments[0].Data["file"].(string), "douyin-test.mp4") {
		t.Fatalf("flush message = %#v", segments)
	}
	if actions.Messages[0].TargetID != "12345" {
		t.Fatalf("flush target = %#v", actions.Messages[0])
	}
	if _, err := os.Stat(segments[0].Data["file"].(string)); !os.IsNotExist(err) {
		t.Fatalf("flushed media temp file was not cleaned up: %v", err)
	}
}

func TestEnsureSchedulerRegistersDedicatedMediaFlush(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.actions = actions

	if !handler.ensureScheduler(context.Background(), &rayleabot.EventContext{}) {
		t.Fatalf("ensureScheduler() = false")
	}
	if len(actions.SchedulerRequests) != 2 {
		t.Fatalf("scheduler requests = %#v", actions.SchedulerRequests)
	}
	if actions.SchedulerRequests[0].TaskID != schedulerTaskID || actions.SchedulerRequests[1].TaskID != deferredMediaTaskID {
		t.Fatalf("scheduler task IDs = %#v", actions.SchedulerRequests)
	}
	if actions.SchedulerRequests[1].Payload["action"] != "flush_deferred_media" {
		t.Fatalf("media scheduler payload = %#v", actions.SchedulerRequests[1].Payload)
	}
}

func TestDeferredMediaSendFailureCleansTempFiles(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	actions.MessageErrors = []error{errors.New("send failed"), nil}
	handler.actions = actions
	tempRoot, err := os.MkdirTemp("", "raylea-deferred-test-*")
	if err != nil {
		t.Fatalf("create temp root: %v", err)
	}
	videoPath := tempRoot + string(os.PathSeparator) + "fixture.mp4"
	if err := os.WriteFile(videoPath, []byte("fixturevideo"), 0o600); err != nil {
		_ = os.RemoveAll(tempRoot)
		t.Fatalf("write fixture: %v", err)
	}
	job := &deferredMediaJob{
		TargetType: "group", TargetID: "12345", Platform: "douyin", tempRoot: tempRoot,
		prepared: []preparedResolverMedia{{Kind: "video", Path: videoPath, Name: "fixture.mp4"}},
	}
	handler.deferredMedia.push(job)

	handler.flushDeferredMedia(context.Background(), &rayleabot.EventContext{})

	if _, err := os.Stat(tempRoot); !os.IsNotExist(err) {
		t.Fatalf("failed media temp root was not cleaned up: %v", err)
	}
	if len(handler.deferredMedia.jobs) != 0 {
		t.Fatalf("failed media job still queued")
	}
	if len(actions.Messages) != 2 {
		t.Fatalf("send failure should emit one failure notice: %#v", actions.Messages)
	}
}

func TestDeferredMediaDownloadFailureNotifies(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.actions = actions
	job := &deferredMediaJob{
		TargetType: "group", TargetID: "12345", Platform: "douyin",
		Sources:  []ResolverMediaSource{{Kind: "video", URLs: []string{"http://127.0.0.1:1/unreachable.mp4"}, FileName: "douyin-test.mp4"}},
		Settings: ResolverMediaSettings{},
	}
	handler.deferredMedia.push(job)
	handler.downloadDeferredMedia(job)
	job.mu.Lock()
	if job.failed == "" {
		job.mu.Unlock()
		t.Fatalf("expected download failure to be recorded")
	}
	job.mu.Unlock()

	handler.flushDeferredMedia(context.Background(), &rayleabot.EventContext{})
	if len(handler.deferredMedia.jobs) != 0 {
		t.Fatalf("failed job still queued")
	}
	if len(actions.Messages) != 1 {
		t.Fatalf("failure notice sent %d messages, want 1", len(actions.Messages))
	}
}
