package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestFFmpegDownloadFallsBackAfterStalledSource(t *testing.T) {
	ffmpeg, err := resolverExecutable("RAYLEABOT_FFMPEG_PATH", "ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is required for the network timeout regression")
	}
	fixture := filepath.Join(t.TempDir(), "source.mp4")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffmpeg, "-y", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=16x16:d=0.2", "-c:v", "mpeg4", fixture).CombinedOutput()
	if err != nil {
		t.Fatalf("generate fixture: %v, %s", err, output)
	}
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		if r.URL.Path == "/stalled" {
			select {
			case <-r.Context().Done():
			case <-ctx.Done():
			}
			return
		}
		http.ServeFile(w, r, fixture)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "download.mp4")
	err = downloadResolverFileWithFFmpeg(ctx, []string{server.URL + "/stalled", server.URL + "/video.mp4"}, nil, destination, 1<<20)
	if err != nil {
		t.Fatalf("download did not recover from stalled source: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Size() == 0 || attempts.Load() < 2 {
		t.Fatalf("fallback download: file=%v, error=%v, attempts=%d", info, err, attempts.Load())
	}
}
