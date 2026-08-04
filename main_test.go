package main

import (
	"reflect"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestNormalizeSubscriptionsRejectsInvalidAndDeduplicates(t *testing.T) {
	valid := subscription{Platform: "bilibili", UID: "42", TargetType: "group", TargetID: "100", Enabled: true}
	items := normalizeSubscriptions([]subscription{valid, valid, subscription{Platform: "unknown", UID: "1", TargetType: "group", TargetID: "2"}})
	if len(items) != 1 || items[0].ID == "" || len(items[0].Services) != 1 || items[0].Services[0] != "all" {
		t.Fatalf("unexpected normalized subscriptions: %#v", items)
	}
}

func TestCommandOperationCoversPlatformMutations(t *testing.T) {
	if platform, operation := commandOperation("订阅b站推送"); platform != "bilibili" || operation != "add" {
		t.Fatalf("unexpected operation: %q %q", platform, operation)
	}
	if platform, operation := commandOperation("全部微博订阅列表"); platform != "weibo" || operation != "list_all" {
		t.Fatalf("unexpected operation: %q %q", platform, operation)
	}
	if platform, operation := commandOperation("b站搜索up"); platform != "bilibili" || operation != "search" {
		t.Fatalf("search command is not wired: %q %q", platform, operation)
	}
	if platform, operation := commandOperation("预览订阅卡片"); platform != "bilibili" || operation != "preview" {
		t.Fatalf("preview command is not wired: %q %q", platform, operation)
	}
}

func TestSubscriptionIDIsStableAndScoped(t *testing.T) {
	first := subscriptionID("bilibili", "42", "group", "100")
	if first != subscriptionID("bilibili", "42", "group", "100") || first == subscriptionID("bilibili", "42", "group", "101") {
		t.Fatalf("unexpected subscription ids")
	}
}

func TestParseSubscriptionArgsNormalizesServiceAndURL(t *testing.T) {
	selected, query, ok := parseSubscriptionArgs([]string{"图文", "https://space.bilibili.com/42"}, "bilibili")
	if !ok || query != "https://space.bilibili.com/42" || !reflect.DeepEqual(selected, []string{"image_text"}) {
		t.Fatalf("unexpected args: %#v %q %v", selected, query, ok)
	}
	if uid := subjectIDFromInput("bilibili", query); uid != "42" {
		t.Fatalf("unexpected Bilibili uid: %q", uid)
	}
	if uid := subjectIDFromInput("netease_music", "https://music.163.com/#/artist?id=123"); uid != "123" {
		t.Fatalf("unexpected NetEase id: %q", uid)
	}
}

func TestServiceMergeAndPartialRemoval(t *testing.T) {
	merged := mergeServices([]string{"video"}, []string{"live"}, "bilibili")
	if !reflect.DeepEqual(sortedServices(merged), []string{"live", "video"}) {
		t.Fatalf("unexpected merged services: %#v", merged)
	}
	remaining := removeServices([]string{"all"}, []string{"live"}, "bilibili")
	if containsService(remaining, "live") || len(remaining) != 4 {
		t.Fatalf("unexpected remaining services: %#v", remaining)
	}
}

func TestRemoveSubscriptionKeepsUnremovedServices(t *testing.T) {
	current := settings{Subscriptions: []subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "UP", TargetType: "group", TargetID: "100",
		Services: []string{"video", "live"}, Enabled: true,
	}}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Target: rayleabot.Target{Type: "group", ID: "100"}, Payload: map[string]any{"args": []string{"直播", "42"}},
	}}
	_, changed := removeSubscription(&current, event, "bilibili")
	if !changed || len(current.Subscriptions) != 1 || !reflect.DeepEqual(current.Subscriptions[0].Services, []string{"video"}) {
		t.Fatalf("partial removal failed: %#v", current.Subscriptions)
	}
}

func TestBilibiliDynamicServiceMapping(t *testing.T) {
	for input, expected := range map[string]string{
		"DYNAMIC_TYPE_AV": "video", "DYNAMIC_TYPE_ARTICLE": "article", "DYNAMIC_TYPE_FORWARD": "repost", "DYNAMIC_TYPE_DRAW": "image_text",
	} {
		if got := bilibiliDynamicService(input); got != expected {
			t.Fatalf("service for %s = %s", input, got)
		}
	}
}
