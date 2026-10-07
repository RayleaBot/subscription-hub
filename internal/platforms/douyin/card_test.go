package douyin

import (
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

// 抖音号（unique_id）可被用户修改，是用户可见的正式标识；sec_uid 是后台
// 参数。所有卡片展示必须优先抖音号，只有缺失时才回退 sec_uid。
func TestDouyinSearchCardUsesUniqueIDText(t *testing.T) {
	users := []douyinUser{
		{UID: "MS4wLjABAAAAone", UniqueID: "testuser", Name: "测试用户"},
	}
	data := buildDouyinSearchCardData("测试用户", users, nil)
	first := plugin.MapSliceValue(data["users"])[0]
	if plugin.StringScalar(first["uid_text"]) != "testuser" {
		t.Fatalf("search card should show unique_id: %#v", first)
	}

	data = buildDouyinSearchCardData("测试用户", []douyinUser{{UID: "MS4wLjABAAAAone", Name: "测试用户"}}, nil)
	first = plugin.MapSliceValue(data["users"])[0]
	if plugin.StringScalar(first["uid_text"]) != "UID MS4wLjABAAAAone" {
		t.Fatalf("search card should fall back to sec_uid: %#v", first)
	}
}

func TestDouyinUserCardPrefersUniqueID(t *testing.T) {
	item := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAone", UniqueID: "stored-id", Name: "测试用户", TargetType: "group", TargetID: "100"}

	// 订阅命令解析出最新抖音号时优先使用。
	data := buildDouyinUserCardData("subscribed", item, douyinUser{UID: "MS4wLjABAAAAone", UniqueID: "fresh-id", Name: "测试用户"}, []string{"video"})
	if data["uid_text"] != "fresh-id" {
		t.Fatalf("user card should prefer resolved unique_id: %#v", data["uid_text"])
	}

	// 解析结果缺抖音号时回退订阅配置里存的。
	data = buildDouyinUserCardData("subscribed", item, douyinUser{UID: "MS4wLjABAAAAone", Name: "测试用户"}, []string{"video"})
	if data["uid_text"] != "stored-id" {
		t.Fatalf("user card should fall back to stored unique_id: %#v", data["uid_text"])
	}

	// 都没有时回退 sec_uid。
	bare := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAone", Name: "测试用户", TargetType: "group", TargetID: "100"}
	data = buildDouyinUserCardData("subscribed", bare, douyinUser{UID: "MS4wLjABAAAAone", Name: "测试用户"}, []string{"video"})
	if data["uid_text"] != "UID MS4wLjABAAAAone" {
		t.Fatalf("user card should fall back to sec_uid: %#v", data["uid_text"])
	}
}

func TestDouyinUpdateCardAuthorUIDTextPrefersUniqueID(t *testing.T) {
	item := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAone", UniqueID: "stored-id", Name: "测试用户"}

	// 作品 feed 带最新抖音号时优先使用。
	data := buildDouyinRenderData(item, map[string]any{
		"author":  map[string]any{"name": "测试用户", "uid": "MS4wLjABAAAAone", "unique_id": "feed-id"},
		"summary": "更新内容", "service": "video",
	})
	if data["author_uid_text"] != "feed-id" {
		t.Fatalf("update card should prefer feed unique_id: %#v", data["author_uid_text"])
	}

	// feed 缺抖音号时回退订阅配置。
	data = buildDouyinRenderData(item, map[string]any{
		"author":  map[string]any{"name": "测试用户", "uid": "MS4wLjABAAAAone"},
		"summary": "更新内容", "service": "video",
	})
	if data["author_uid_text"] != "stored-id" {
		t.Fatalf("update card should fall back to stored unique_id: %#v", data["author_uid_text"])
	}

	// 都没有时留空，模板回退 sec_uid 并加 "UID " 前缀。
	bare := plugin.Subscription{Platform: "douyin", UID: "MS4wLjABAAAAone", Name: "测试用户"}
	data = buildDouyinRenderData(bare, map[string]any{
		"author":  map[string]any{"name": "测试用户", "uid": "MS4wLjABAAAAone"},
		"summary": "更新内容", "service": "video",
	})
	if data["author_uid_text"] != "" {
		t.Fatalf("update card should leave author_uid_text empty for template fallback: %#v", data["author_uid_text"])
	}
}
