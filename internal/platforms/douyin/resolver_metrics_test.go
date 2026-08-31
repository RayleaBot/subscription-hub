package douyin

import (
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func TestDouyinPreviewBuildsResolverMetrics(t *testing.T) {
	update := normalizeDouyinAweme(map[string]any{
		"aweme_id": "7000000000000000000", "desc": "测试作品", "create_time": 1700000000,
		"author": map[string]any{"sec_uid": "MS4wLjABAAAAmetric", "nickname": "测试用户"},
		"video":  map[string]any{"duration": 80000},
		"statistics": map[string]any{
			"digg_count": 96000, "collect_count": 68000, "comment_count": 8600, "share_count": 12000,
		},
	})
	data := buildDouyinRenderData(plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAmetric", Name: "测试用户"}, update)
	metrics := plugin.MapSliceValue(data["metrics"])
	if len(metrics) != 4 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics[0]["label"] != "点赞" || metrics[0]["value_text"] != "9.6万" || metrics[3]["label"] != "分享" || metrics[3]["value_text"] != "1.2万" {
		t.Fatalf("unexpected metric projection: %#v", metrics)
	}
}
