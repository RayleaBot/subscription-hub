package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
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
		SubjectName: "抖音作者", SenderID: "20002", SenderName: "链接发送人", SourceAdapter: "qq-main",
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
	if len(actions.Messages) != 0 || len(actions.ForwardRequests) != 1 {
		t.Fatalf("flush delivery = %#v, %#v", actions.Messages, actions.ForwardRequests)
	}
	request := actions.ForwardRequests[0]
	media := forwardMedia(request, 0)
	path := StringScalar(NestedValue(media, "data", "file"))
	if media["type"] != "video" || !strings.Contains(path, "douyin-test.mp4") {
		t.Fatalf("flush message = %#v", media)
	}
	if request["target_id"] != "12345" || request["source"] != "抖音作者" || request["source_adapter"] != "qq-main" || forwardNode(request, 0)["uin"] != "20002" || forwardNode(request, 0)["name"] != "链接发送人" {
		t.Fatalf("flush identity = %#v", request)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
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
	if len(actions.SchedulerRequests) != 3 {
		t.Fatalf("scheduler requests = %#v", actions.SchedulerRequests)
	}
	if actions.SchedulerRequests[0].TaskID != schedulerTaskID || actions.SchedulerRequests[1].TaskID != deferredMediaTaskID || actions.SchedulerRequests[2].TaskID != accountCheckTaskID {
		t.Fatalf("scheduler task IDs = %#v", actions.SchedulerRequests)
	}
	if actions.SchedulerRequests[1].Payload["action"] != "flush_deferred_media" {
		t.Fatalf("media scheduler payload = %#v", actions.SchedulerRequests[1].Payload)
	}
}

func TestDeferredMediaSendFailureCleansTempFiles(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	actions.ForwardErrors = []error{errors.New("send failed")}
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
	if len(actions.Messages) != 1 || len(actions.ForwardRequests) != 1 {
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
	assertMediaFailureIsPrivate(t, actions, "127.0.0.1:1")
}

func TestDeferredMediaCacheFailureKeepsLocalPathInLogs(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.actions = actions
	blocked := filepath.Join(t.TempDir(), "private-media-cache")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler.deferredMedia = newDeferredMediaQueue(blocked)
	job := &deferredMediaJob{TargetType: "group", TargetID: "fixture-group", Platform: "douyin", SourceAdapter: "fixture-adapter"}
	if !handler.deferredMedia.push(job) {
		t.Fatal("queue push failed")
	}
	handler.downloadDeferredMedia(job)
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	assertMediaFailureIsPrivate(t, actions, blocked)
	if len(handler.deferredMedia.jobs) != 0 || actions.Messages[0].SourceAdapter != job.SourceAdapter {
		t.Fatal("failed job was not removed or notice lost its source adapter")
	}
}

func TestDeferredMediaFailureRedactsCredentialsInLogs(t *testing.T) {
	handler := newWorkflowHandler(t)
	actions := testkit.NewActions()
	handler.sendDeferredFailure(t.Context(), actions, &deferredMediaJob{TargetType: "group", TargetID: "fixture-group"}, "open /private/media.mp4: Access is denied; access_token=fixture-sensitive-token")
	assertMediaFailureIsPrivate(t, actions, "/private/media.mp4")
	if strings.Contains(StringScalar(actions.Logs[0].Fields["error"]), "fixture-sensitive-token") {
		t.Fatal("media diagnostics leaked a credential")
	}
}

func TestMediaCacheUsesHostDirectory(t *testing.T) {
	cacheRoot := filepath.Join(t.TempDir(), "cache", "plugins", "raylea.subscription-hub")
	t.Setenv("RAYLEABOT_PLUGIN_CACHE_DIR", cacheRoot)
	handler, err := NewHandler(Options{Platforms: []Platform{workflowPlatform("fixture", &workflowSession{})}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.accountQR.Close)
	jobRoot, err := handler.deferredMedia.createTemp()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(jobRoot) != filepath.Join(cacheRoot, "media") {
		t.Fatalf("media cache escaped the host cache: %q", jobRoot)
	}
	if _, err := os.Stat(filepath.Join(jobRoot, mediaLeaseName)); err != nil {
		t.Fatalf("media lease not created: %v", err)
	}
}

func TestMediaCacheRequiresAbsoluteHostDirectory(t *testing.T) {
	for _, root := range []string{"", "relative-cache"} {
		t.Run(root, func(t *testing.T) {
			t.Setenv("RAYLEABOT_PLUGIN_CACHE_DIR", root)
			if _, err := NewHandler(Options{Platforms: []Platform{workflowPlatform("fixture", &workflowSession{})}}); err == nil {
				t.Fatal("missing or relative host cache must not fall back to the system temp directory")
			}
		})
	}
}
