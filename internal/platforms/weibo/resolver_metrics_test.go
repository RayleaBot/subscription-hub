package weibo

import (
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func TestWeiboPreviewBuildsResolverMetrics(t *testing.T) {
	mblog := weiboTextMblog("metric-1", "6000000001", "测试微博", 1700000000)
	mblog["reposts_count"] = 12000
	mblog["comments_count"] = 8600
	mblog["attitudes_count"] = 96000
	update := normalizeWeiboMblog(time.UTC, mblog, 0)
	data := buildWeiboRenderData(plugin.Subscription{Platform: "weibo", UID: "6000000001", Name: "测试博主"}, update)
	metrics := plugin.MapSliceValue(data["metrics"])
	if len(metrics) != 3 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics[0]["label"] != "转发" || metrics[0]["value_text"] != "1.2万" || metrics[2]["label"] != "点赞" || metrics[2]["value_text"] != "9.6万" {
		t.Fatalf("unexpected metric projection: %#v", metrics)
	}
}
