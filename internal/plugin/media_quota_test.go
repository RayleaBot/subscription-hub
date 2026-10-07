package plugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestEmptyRetainedLeasesDoNotExhaustMediaQuota(t *testing.T) {
	root := t.TempDir()
	old := newDeferredMediaQueue(root)
	for range deferredMediaMaxJobs + 1 {
		if _, err := old.createTemp(); err != nil {
			t.Fatal(err)
		}
	}
	restarted := newDeferredMediaQueue(root)
	for index := range deferredMediaMaxJobs {
		activeRoot, err := restarted.createTemp()
		if err != nil {
			t.Fatal(err)
		}
		// 正在准备的任务即使还未写入媒体，也必须占用任务槽。
		if !restarted.push(&deferredMediaJob{tempRoot: activeRoot}) {
			t.Fatalf("empty retained records blocked new media job %d", index)
		}
	}
	if restarted.push(&deferredMediaJob{}) {
		t.Fatal("active media limit was bypassed")
	}
	if len(restarted.readyJobs()) != 0 {
		t.Fatal("empty retained records were queued for sending")
	}
}

func TestRetainedMediaQuotaRecoversWhenOnlyLeaseMarkersRemain(t *testing.T) {
	queue := newDeferredMediaQueue(t.TempDir())
	paths := make([]string, 0, deferredMediaMaxJobs)
	for range deferredMediaMaxJobs {
		root, err := queue.createTemp()
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(root, "image.png")
		if err := os.WriteFile(file, []byte("retained image"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, file)
		if !queue.push(&deferredMediaJob{tempRoot: root, retainUntil: time.Now().Add(mediaRetention)}) {
			t.Fatal("retained media rejected prematurely")
		}
	}
	if queue.push(&deferredMediaJob{}) {
		t.Fatal("real retained files lost quota protection")
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(paths[0]), mediaLeaseName+".next"), []byte("interrupted marker update"), 0600); err != nil {
		t.Fatal(err)
	}
	if !queue.push(&deferredMediaJob{}) {
		t.Fatal("lease metadata still consumed the freed media slot")
	}
	for _, path := range paths[1:] {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unconfirmed file was removed: %v", err)
		}
	}
}

func TestEmptyRetainedLeasesAllowBackgroundDownloadAndFlush(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fixturevideo"))
	}))
	defer server.Close()
	handler := newWorkflowHandler(t)
	root := handler.deferredMedia.root
	for range deferredMediaMaxJobs + 1 {
		if _, err := handler.deferredMedia.createTemp(); err != nil {
			t.Fatal(err)
		}
	}
	handler.deferredMedia = newDeferredMediaQueue(root)
	actions := testkit.NewActions()
	handler.actions = actions
	target := resolverSendTarget{TargetType: "group", TargetID: "12345", SubjectName: "视频作者", SenderID: "20002", SenderName: "链接发送人"}
	plan := ResolverMediaPlan{Sources: []ResolverMediaSource{{Kind: "video", URLs: []string{server.URL}, FileName: "fixture.mp4"}}}
	if !handler.enqueueDeferredMedia(target, "douyin", plan, ResolverMediaSettings{}) {
		t.Fatal("empty retained records blocked the background download")
	}
	waitMediaJobs(t, handler, 1)
	handler.flushDeferredMedia(t.Context(), &rayleabot.EventContext{})
	if len(actions.Messages) != 0 || len(actions.ForwardRequests) != 1 {
		t.Fatalf("background delivery = %#v, %#v", actions.Messages, actions.ForwardRequests)
	}
	media := forwardMedia(actions.ForwardRequests[0], 0)
	if media["type"] != "video" {
		t.Fatalf("background media = %#v", media)
	}
	if _, err := os.Stat(StringScalar(NestedValue(media, "data", "file"))); !os.IsNotExist(err) {
		t.Fatalf("confirmed background media was not cleaned up: %v", err)
	}
	for range deferredMediaMaxJobs {
		if !handler.deferredMedia.push(&deferredMediaJob{}) {
			t.Fatal("completed background download still occupied a media slot")
		}
	}
}
