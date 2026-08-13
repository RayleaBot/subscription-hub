package plugin

import (
	"reflect"
	"strings"
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
	if platform, operation := commandOperation("B站搜索UP"); platform != "bilibili" || operation != "search" {
		t.Fatalf("search alias is not wired: %q %q", platform, operation)
	}
	if platform, operation := commandOperation("微博搜索博主"); platform != "weibo" || operation != "search" {
		t.Fatalf("weibo search command is not wired: %q %q", platform, operation)
	}
	if platform, operation := commandOperation("预览订阅卡片"); platform != "bilibili" || operation != "preview" {
		t.Fatalf("preview command is not wired: %q %q", platform, operation)
	}
}

func TestInteractiveCardCommandsReserveReplyBudget(t *testing.T) {
	for _, operation := range []string{"add", "remove", "search", "preview"} {
		if !interactiveCommandOperation(operation) {
			t.Fatalf("%s should share the interactive reply budget", operation)
		}
	}
	for _, operation := range []string{"status", "list", "list_all"} {
		if interactiveCommandOperation(operation) {
			t.Fatalf("%s should not receive the card rendering budget", operation)
		}
	}
}

func TestStatusAndSubscriptionListPreserveOperationalDetails(t *testing.T) {
	current := settings{Enabled: true, Subscriptions: []subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "测试UP", TargetType: "group", TargetID: "100",
		Services: []string{"video"}, Subscribers: []subscriber{{ID: "7", Nickname: "柒柒"}}, Enabled: true,
	}}}
	if status := formatStatus(current); !strings.Contains(status, "检查：") || !strings.Contains(status, "账号：") {
		t.Fatalf("status omitted operational guidance: %q", status)
	}
	event := &rayleabot.EventContext{Event: rayleabot.Event{Target: rayleabot.Target{Type: "group", ID: "100"}}}
	list := formatSubscriptions(current, event, "bilibili", false)
	for _, expected := range []string{"Bilibili 订阅列表", "群聊 100", "测试UP（UID 42）", "订阅人：柒柒"} {
		if !strings.Contains(list, expected) {
			t.Fatalf("subscription list %q omitted %q", list, expected)
		}
	}
}

func TestSubscriptionListTitlesPreserveLegacySpacing(t *testing.T) {
	for _, test := range []struct {
		platform string
		all      bool
		want     string
	}{
		{platform: "bilibili", all: true, want: "全部 Bilibili 订阅列表"},
		{platform: "weibo", all: true, want: "全部微博订阅列表"},
		{platform: "douyin", want: "抖音订阅列表"},
		{all: true, want: "全部订阅列表"},
	} {
		if got := subscriptionListTitle(test.platform, test.all); got != test.want {
			t.Fatalf("subscriptionListTitle(%q, %v) = %q, want %q", test.platform, test.all, got, test.want)
		}
	}
}

func TestNonBilibiliSubscriptionKeepsOriginalSubjectName(t *testing.T) {
	current := settings{}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"洛天依"}},
	}}
	if outcome := addSubscription(t.Context(), &current, event, "netease_music"); !outcome.Changed {
		t.Fatal("subscription was not added")
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscription subject name = %#v", current.Subscriptions)
	}
}

func TestCurrentTargetNameFallsBackToOneBotAndPrivateActor(t *testing.T) {
	group := &rayleabot.EventContext{Event: rayleabot.Event{
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"onebot": map[string]any{"group_name": "测试群"}},
	}}
	if got := currentTargetName(group); got != "测试群" {
		t.Fatalf("group target name = %q", got)
	}
	private := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor: rayleabot.Actor{Nickname: "柒柒"}, Target: rayleabot.Target{Type: "private", ID: "7"},
	}}
	if got := currentTargetName(private); got != "柒柒" {
		t.Fatalf("private target name = %q", got)
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
	outcome := removeSubscription(&current, event, "bilibili")
	if !outcome.Changed || len(current.Subscriptions) != 1 || !reflect.DeepEqual(current.Subscriptions[0].Services, []string{"video"}) {
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

func TestNormalizeDeliveryMaxAgeMinutes(t *testing.T) {
	for name, test := range map[string]struct {
		value int
		want  int
	}{
		"missing uses default": {value: 0, want: 30},
		"valid is preserved":   {value: 90, want: 90},
		"maximum is clamped":   {value: 2000, want: 1440},
	} {
		t.Run(name, func(t *testing.T) {
			if got := normalizeDeliveryMaxAgeMinutes(test.value); got != test.want {
				t.Fatalf("normalizeDeliveryMaxAgeMinutes(%d) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
