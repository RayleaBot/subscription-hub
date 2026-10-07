package bilibili

import (
	"testing"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func TestBilibiliVideoPreviewBuildsResolverMetrics(t *testing.T) {
	update, err := previewVideoUpdate(time.UTC, map[string]any{"data": map[string]any{
		"bvid": "BV1Metrics", "title": "测试视频", "duration": 768,
		"owner": map[string]any{"mid": 42, "name": "测试 UP"},
		"stat": map[string]any{
			"view": 1286000, "like": 96000, "coin": 52000, "favorite": 68000,
			"share": 12000, "reply": 8600, "danmaku": 32000,
		},
	}}, "https://www.bilibili.com/video/BV1Metrics")
	if err != nil {
		t.Fatalf("preview video: %v", err)
	}
	data := buildBilibiliRenderData(plugin.Subscription{Platform: "bilibili", UID: "42", Name: "测试 UP"}, update)
	metrics := plugin.MapSliceValue(data["metrics"])
	if len(metrics) != 7 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics[0]["label"] != "播放" || metrics[0]["value_text"] != "128.6万" || metrics[6]["label"] != "弹幕" || metrics[6]["value_text"] != "3.2万" {
		t.Fatalf("unexpected metric projection: %#v", metrics)
	}
}
