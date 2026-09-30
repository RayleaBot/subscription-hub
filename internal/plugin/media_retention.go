package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const mediaRetention = 24 * time.Hour
const mediaLeaseName = ".raylea-media-lease.json"

type mediaLease struct {
	Owner string    `json:"owner"`
	Until time.Time `json:"until"`
}

func mediaOutcomeUncertain(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		switch actionErr.Code {
		case "adapter.send_unconfirmed", "adapter.api_call_failed", "adapter.http_api_request_failed", "adapter.http_api_invalid_response", "plugin.shutdown", "plugin.internal_error", "plugin.event_canceled", "plugin.event_timeout":
			return true
		}
	}
	return false
}

func (queue *deferredMediaQueue) createTemp() (string, error) {
	if err := os.MkdirAll(queue.root, 0o700); err != nil {
		return "", err
	}
	root, err := os.MkdirTemp(queue.root, "job-")
	if err != nil {
		return "", err
	}
	// The lease exists before any path is exposed to the adapter. A process
	// restart retains this resource without replaying its uncertain send.
	data, err := json.Marshal(mediaLease{Owner: "raylea.subscription-hub", Until: time.Now().Add(mediaRetention + deferredMediaDownloadTimeout + 2*interactiveReplyTimeout)})
	if err == nil {
		err = os.WriteFile(filepath.Join(root, mediaLeaseName), data, 0o600)
	}
	if err != nil {
		_ = os.RemoveAll(root)
		return "", err
	}
	return root, nil
}

func (job *deferredMediaJob) retain() {
	job.mu.Lock()
	defer job.mu.Unlock()
	job.retainUntil = time.Now().Add(mediaRetention)
	data, err := json.Marshal(mediaLease{Owner: "raylea.subscription-hub", Until: job.retainUntil})
	if err != nil || job.tempRoot == "" {
		return
	}
	marker := filepath.Join(job.tempRoot, mediaLeaseName)
	temporary := marker + ".next"
	if err := os.WriteFile(temporary, data, 0o600); err == nil {
		if err := os.Rename(temporary, marker); err != nil {
			_ = os.Remove(temporary)
		}
	}
	// A failed update keeps the conservative initial lease valid across restart.
}

func (job *deferredMediaJob) occupiesMediaSlot() bool {
	job.mu.Lock()
	retained, root := !job.retainUntil.IsZero(), job.tempRoot
	job.mu.Unlock()
	if !retained {
		// 下载开始前也要预留任务槽，不能根据目录是否为空判断活动任务。
		return true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		// 无法检查目录时保守保留配额；不存在的目录没有待保留媒体。
		return !os.IsNotExist(err)
	}
	for _, entry := range entries {
		if entry.Name() != mediaLeaseName && entry.Name() != mediaLeaseName+".next" {
			return true
		}
	}
	// 空记录仍按原期限清理，不占媒体配额，也不提前删除保留标记。
	return false
}

func (queue *deferredMediaQueue) loadRetained() {
	entries, err := os.ReadDir(queue.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "job-") {
			continue
		}
		root := filepath.Join(queue.root, entry.Name())
		data, err := os.ReadFile(filepath.Join(root, mediaLeaseName))
		var lease mediaLease
		if err != nil || json.Unmarshal(data, &lease) != nil || lease.Owner != "raylea.subscription-hub" || lease.Until.IsZero() {
			continue
		}
		queue.jobs = append(queue.jobs, &deferredMediaJob{tempRoot: root, retainUntil: lease.Until})
	}
	queue.pruneRetainedLocked(time.Now())
}

func (queue *deferredMediaQueue) pruneRetainedLocked(now time.Time) {
	kept := queue.jobs[:0]
	for _, job := range queue.jobs {
		job.mu.Lock()
		expired := !job.retainUntil.IsZero() && !now.Before(job.retainUntil)
		retained, root := !job.retainUntil.IsZero(), job.tempRoot
		job.mu.Unlock()
		if retained {
			if _, err := os.Lstat(root); os.IsNotExist(err) {
				continue
			}
		}
		if expired {
			job.cleanup()
		} else {
			kept = append(kept, job)
		}
	}
	queue.jobs = kept
}

type partialMediaSendError struct{ error }

func (err *partialMediaSendError) Unwrap() error { return err.error }
