package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/platforms/bilibili"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestNormalizeSubscriptionsRejectsInvalidAndDeduplicates(t *testing.T) {
	valid := plugin.Subscription{Platform: "bilibili", UID: "42", TargetType: "group", TargetID: "100", Enabled: true}
	items := newHandler(t).NormalizeSubscriptions([]plugin.Subscription{valid, valid, plugin.Subscription{Platform: "unknown", UID: "1", TargetType: "group", TargetID: "2"}})
	if len(items) != 1 || items[0].ID == "" || len(items[0].Services) != 1 || items[0].Services[0] != "all" {
		t.Fatalf("unexpected normalized subscriptions: %#v", items)
	}
}

func TestNormalizeSubscriptionSubscribersRepairsStoredIdentity(t *testing.T) {
	items := []plugin.Subscription{{Subscribers: []plugin.Subscriber{{
		ID: "2678980697", Nickname: "柒柒", Role: "member", RoleLabel: "成员",
		AvatarURL: "https://q1.qlogo.cn/g?b=qq&nk=2678980697&s=640",
	}}}}

	plugin.NormalizeSubscriptionSubscribers(items, []string{"2678980697"})

	got := items[0].Subscribers[0]
	if got.BaseRole != "member" || got.Role != "super_admin" || got.RoleLabel != "超级管理员" {
		t.Fatalf("stored role was not repaired: %#v", got)
	}
	if got.AvatarURL != "https://q1.qlogo.cn/g?b=qq&nk=2678980697&s=100" {
		t.Fatalf("stored oversized avatar was not normalized: %q", got.AvatarURL)
	}
}

func TestMergeSubscriberUsesLatestRoleAndPreservesProfile(t *testing.T) {
	items := []plugin.Subscriber{{
		ID: "2678980697", Nickname: "柒柒", GroupNickname: "银蝶", Title: "专属头衔",
		BaseRole: "admin", Role: "super_admin", RoleLabel: "超级管理员",
	}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:  rayleabot.Actor{ID: "2678980697", Role: "member"},
		Target: rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"onebot": map[string]any{
			"sender": map[string]any{"user_id": "2678980697", "nickname": "柒柒", "role": "member"},
		}},
	}}

	got := plugin.MergeSubscriber(items, event)[0]
	if got.BaseRole != "member" || got.Role != "member" || got.RoleLabel != "群员" {
		t.Fatalf("subscriber role was not refreshed: %#v", got)
	}
	if got.GroupNickname != "银蝶" || got.Title != "专属头衔" {
		t.Fatalf("richer stored identity was lost: %#v", got)
	}
	if got.AvatarURL != "https://q1.qlogo.cn/g?b=qq&nk=2678980697&s=100" {
		t.Fatalf("subscriber avatar was not normalized: %q", got.AvatarURL)
	}
}

func TestNormalizeSubscriptionSubscribersRestoresBaseRoleAfterSuperAdminRemoval(t *testing.T) {
	items := []plugin.Subscription{{Subscribers: []plugin.Subscriber{{
		ID: "2678980697", Nickname: "柒柒", BaseRole: "admin", Role: "super_admin", RoleLabel: "超级管理员",
	}}}}

	plugin.NormalizeSubscriptionSubscribers(items, nil)

	got := items[0].Subscribers[0]
	if got.BaseRole != "admin" || got.Role != "admin" || got.RoleLabel != "管理员" {
		t.Fatalf("removed super admin did not recover the current group role: %#v", got)
	}
}

func TestCommandOperationCoversPlatformMutations(t *testing.T) {
	if platform, operation := newHandler(t).CommandOperation("订阅b站推送"); platform != "bilibili" || operation != "add" {
		t.Fatalf("unexpected operation: %q %q", platform, operation)
	}
	if platform, operation := newHandler(t).CommandOperation("全部微博订阅列表"); platform != "weibo" || operation != "list_all" {
		t.Fatalf("unexpected operation: %q %q", platform, operation)
	}
	if platform, operation := newHandler(t).CommandOperation("b站搜索up"); platform != "bilibili" || operation != "search" {
		t.Fatalf("search command is not wired: %q %q", platform, operation)
	}
	if platform, operation := newHandler(t).CommandOperation("B站搜索UP"); platform != "bilibili" || operation != "search" {
		t.Fatalf("search alias is not wired: %q %q", platform, operation)
	}
	if platform, operation := newHandler(t).CommandOperation("微博搜索博主"); platform != "weibo" || operation != "search" {
		t.Fatalf("weibo search command is not wired: %q %q", platform, operation)
	}
	if platform, operation := newHandler(t).CommandOperation("预览订阅卡片"); platform != "" || operation != "preview" {
		t.Fatalf("preview command is not wired: %q %q", platform, operation)
	}
}

func TestInteractiveCommandsReserveReplyBudget(t *testing.T) {
	for _, operation := range []string{"add", "remove", "search", "preview", "check"} {
		if !plugin.InteractiveCommandOperation(operation) {
			t.Fatalf("%s should share the interactive reply budget", operation)
		}
	}
	for _, operation := range []string{"status", "list", "list_all"} {
		if plugin.InteractiveCommandOperation(operation) {
			t.Fatalf("%s should not receive the card rendering budget", operation)
		}
	}
}

func TestStatusAndSubscriptionListPreserveOperationalDetails(t *testing.T) {
	current := plugin.Settings{Enabled: true, Subscriptions: []plugin.Subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "测试UP", TargetType: "group", TargetID: "100",
		Services: []string{"video"}, Subscribers: []plugin.Subscriber{{ID: "7", Nickname: "柒柒"}}, Enabled: true,
	}}}
	if status := newHandler(t).FormatStatus(current); !strings.Contains(status, "检查：") || !strings.Contains(status, "账号：") {
		t.Fatalf("status omitted operational guidance: %q", status)
	}
	event := &rayleabot.EventContext{Event: rayleabot.Event{Target: rayleabot.Target{Type: "group", ID: "100"}}}
	list := newHandler(t).FormatSubscriptions(current, event, "bilibili", false)
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
		if got := newHandler(t).SubscriptionListTitle(test.platform, test.all); got != test.want {
			t.Fatalf("subscriptionListTitle(%q, %v) = %q, want %q", test.platform, test.all, got, test.want)
		}
	}
}

func TestNonBilibiliSubscriptionKeepsOriginalSubjectName(t *testing.T) {
	current := plugin.Settings{}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"洛天依"}},
	}}
	if outcome := newHandler(t).AddSubscription(t.Context(), testkit.NewActions(), &current, event, "netease_music"); !outcome.Changed {
		t.Fatal("subscription was not added")
	}
	if len(current.Subscriptions) != 1 || current.Subscriptions[0].Name != "洛天依" {
		t.Fatalf("subscription subject name = %#v", current.Subscriptions)
	}
}

func TestCurrentTargetNameUsesTargetNameAndPrivateActor(t *testing.T) {
	group := &rayleabot.EventContext{Event: rayleabot.Event{
		Target: rayleabot.Target{Type: "group", ID: "100", Name: "测试群"},
	}}
	if got := plugin.CurrentTargetName(group); got != "测试群" {
		t.Fatalf("group target name = %q", got)
	}
	// payload.onebot is a closed projection without group_name, so a group
	// event that carries no Target.Name has no other source for one.
	unnamed := &rayleabot.EventContext{Event: rayleabot.Event{
		Target: rayleabot.Target{Type: "group", ID: "100"},
	}}
	if got := plugin.CurrentTargetName(unnamed); got != "" {
		t.Fatalf("unnamed group target name = %q", got)
	}
	private := &rayleabot.EventContext{Event: rayleabot.Event{
		Actor: rayleabot.Actor{Nickname: "柒柒"}, Target: rayleabot.Target{Type: "private", ID: "7"},
	}}
	if got := plugin.CurrentTargetName(private); got != "柒柒" {
		t.Fatalf("private target name = %q", got)
	}
}

func TestSubscriptionIDIsStableAndScoped(t *testing.T) {
	first := plugin.SubscriptionID("bilibili", "42", "group", "100")
	if first != plugin.SubscriptionID("bilibili", "42", "group", "100") || first == plugin.SubscriptionID("bilibili", "42", "group", "101") {
		t.Fatalf("unexpected subscription ids")
	}
}

func TestParseSubscriptionArgsNormalizesServiceAndURL(t *testing.T) {
	selected, query, ok := newHandler(t).ParseSubscriptionArgs([]string{"图文", "https://space.bilibili.com/42"}, "bilibili")
	if !ok || query != "https://space.bilibili.com/42" || !reflect.DeepEqual(selected, []string{"image_text"}) {
		t.Fatalf("unexpected args: %#v %q %v", selected, query, ok)
	}
	if uid := newHandler(t).SubjectIDFromInput("bilibili", query); uid != "42" {
		t.Fatalf("unexpected Bilibili uid: %q", uid)
	}
	if uid := newHandler(t).SubjectIDFromInput("netease_music", "https://music.163.com/#/artist?id=123"); uid != "123" {
		t.Fatalf("unexpected NetEase id: %q", uid)
	}
}

func TestServiceMergeAndPartialRemoval(t *testing.T) {
	merged := bilibili.New().Services.Merge([]string{"video"}, []string{"live"})
	if !reflect.DeepEqual(plugin.SortedServices(merged), []string{"live", "video"}) {
		t.Fatalf("unexpected merged services: %#v", merged)
	}
	remaining := bilibili.New().Services.Remove([]string{"all"}, []string{"live"})
	if plugin.ContainsService(remaining, "live") || len(remaining) != 4 {
		t.Fatalf("unexpected remaining services: %#v", remaining)
	}
}

func TestRemoveSubscriptionKeepsUnremovedServices(t *testing.T) {
	current := plugin.Settings{Subscriptions: []plugin.Subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "UP", TargetType: "group", TargetID: "100",
		Services: []string{"video", "live"}, Enabled: true,
	}}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Target: rayleabot.Target{Type: "group", ID: "100"}, Payload: map[string]any{"args": []string{"直播", "42"}},
	}}
	outcome := newHandler(t).RemoveSubscription(&current, event, "bilibili")
	if !outcome.Changed || len(current.Subscriptions) != 1 || !reflect.DeepEqual(current.Subscriptions[0].Services, []string{"video"}) {
		t.Fatalf("partial removal failed: %#v", current.Subscriptions)
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
			if got := plugin.NormalizeDeliveryMaxAgeMinutes(test.value); got != test.want {
				t.Fatalf("normalizeDeliveryMaxAgeMinutes(%d) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}

func newHandler(t testing.TB) *plugin.Handler {
	t.Helper()
	handler, err := plugin.NewHandler(plugin.Options{Platforms: platforms(), MediaTempRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestRuntimeRoutesManagementCommandsAndPreviewsThroughSharedHandler(t *testing.T) {
	for _, aliases := range []bool{false, true} {
		t.Run(fmt.Sprintf("aliases=%t", aliases), func(t *testing.T) {
			testRuntimeRoutesManagementCommandsAndPreviews(t, aliases)
		})
	}
}

func testRuntimeRoutesManagementCommandsAndPreviews(t *testing.T, aliases bool) {
	t.Helper()
	subscribeCommand, previewCommand := "订阅网易云音乐推送", "预览订阅卡片"
	if aliases {
		subscribeCommand, previewCommand = "订阅网易云", "订阅预览"
	}
	actions := testkit.NewActions()
	actions.Config = map[string]any{
		"enabled": true, "delivery_max_age_minutes": 30,
		"subscriptions": []any{}, "resolver": map[string]any{},
	}
	actions.HTTPDefault = testkit.AvatarHTTPResult()
	handler, err := plugin.NewHandler(plugin.Options{Platforms: platforms(), Actions: actions, MediaTempRoot: t.TempDir(), Jitter: func() time.Duration { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	defer func() {
		_ = outputWriter.Close()
		_ = outputReader.Close()
		_ = inputWriter.Close()
		_ = inputReader.Close()
	}()
	done := make(chan error, 1)
	go func() {
		done <- rayleabot.Run(ctx, rayleabot.Options{
			Stdin: inputReader, Stdout: outputWriter, Stderr: io.Discard,
		}, handler)
		_ = inputReader.Close()
		_ = outputWriter.Close()
	}()
	frames := make(chan map[string]any, 16)
	decodeErrors := make(chan error, 1)
	go func() {
		defer close(frames)
		decoder := json.NewDecoder(outputReader)
		for {
			frame := map[string]any{}
			if err := decoder.Decode(&frame); err != nil {
				if err != io.EOF {
					decodeErrors <- err
				}
				return
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()
	encoder := json.NewEncoder(inputWriter)
	write := func(kind, id string, event any) {
		t.Helper()
		frame := map[string]any{"type": kind, "request_id": id}
		if kind == "init" {
			frame["protocol_version"] = "4"
			frame["plugin_id"] = "raylea.subscription-hub"
			frame["config"] = actions.Config
			frame["super_admins"] = []string{"7"}
			frame["command_prefixes"] = []string{"/"}
			frame["concurrency"] = 1
			frame["timezone"] = "Asia/Shanghai"
			frame["bots"] = []any{map[string]any{"source_adapter": "fixture", "source_protocol": "onebot11", "id": "1001"}}
		}
		if event != nil {
			frame["event"] = event
			frame["deadline_at_ms"] = time.Now().Add(5 * time.Second).UnixMilli()
		}
		if kind == "shutdown" {
			frame["reason"] = "stop"
		}
		if err := encoder.Encode(frame); err != nil {
			t.Fatal(err)
		}
	}
	read := func(id string) map[string]any {
		t.Helper()
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatal("runtime stopped before replying")
			}
			if frame["request_id"] != id || frame["type"] == "error" {
				t.Fatalf("unexpected terminal frame: %#v", frame)
			}
			return frame
		case err := <-decodeErrors:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal("runtime reply timed out")
		}
		return nil
	}
	write("init", "init", nil)
	if frame := read("init"); frame["type"] != "init_ack" || frame["status"] != "ready" {
		t.Fatalf("plugin did not initialize: %#v", frame)
	}
	event := func(kind string, payload map[string]any) map[string]any {
		if payload == nil {
			payload = map[string]any{}
		}
		return map[string]any{
			"event_id": "event-fixture", "source_protocol": "onebot11", "source_adapter": "fixture",
			"event_type": kind, "timestamp": time.Now().Unix(),
			"actor":   map[string]any{"id": "7", "nickname": "fixture"},
			"target":  map[string]any{"type": "group", "id": "100"},
			"payload": payload,
		}
	}
	write("event", "start", event("plugin.started", nil))
	read("start")
	write("event", "resolve", event("management.action", map[string]any{"action": "subscription.resolve_user", "payload": map[string]any{"platform": "netease_music", "query": "https://music.163.com/#/artist?id=42"}}))
	resolved := read("resolve")
	if !plugin.BoolScalar(plugin.NestedValue(resolved, "data", "exact")) || plugin.StringScalar(plugin.NestedValue(resolved, "data", "user", "uid")) != "42" {
		t.Fatalf("management resolution = %#v", resolved)
	}
	write("event", "subscribe", event("message.group", map[string]any{"command": subscribeCommand, "args": []string{"音乐人", "https://music.163.com/#/artist?id=42"}}))
	if frame := read("subscribe"); frame["action"] != "message.send" {
		t.Fatalf("subscription reply = %#v", frame)
	}
	write("event", "config-refresh", event("config.changed", map[string]any{"config": actions.Config, "changed_keys": []string{"subscriptions"}}))
	read("config-refresh")
	write("event", "check", event("management.action", map[string]any{"action": "subscription.check_now"}))
	if frame := read("check"); plugin.StringScalar(plugin.NestedValue(frame, "data", "skipped")) != "no_checkable_subscriptions" {
		t.Fatalf("NetEase gained a check capability: %#v", frame)
	}
	if len(actions.HTTPRequests) != 0 || len(actions.Renders) != 0 {
		t.Fatal("NetEase subscription performed source requests or card rendering")
	}
	for index, preview := range []string{"b站 视频", "微博 图片", "抖音 直播"} {
		id := fmt.Sprintf("preview-%d", index)
		write("event", id, event("message.group", map[string]any{"command": previewCommand, "args": []string{preview}}))
		if frame := read(id); frame["action"] != "message.send" {
			t.Fatalf("preview reply = %#v", frame)
		}
	}
	write("event", "scheduled", event("scheduler.trigger", map[string]any{"action": "check_subscriptions"}))
	if frame := read("scheduled"); plugin.StringScalar(plugin.NestedValue(frame, "data", "skipped")) != "no_checkable_subscriptions" {
		t.Fatalf("scheduler used a different check flow: %#v", frame)
	}
	write("event", "media-flush", event("scheduler.trigger", map[string]any{"action": "flush_deferred_media"}))
	if frame := read("media-flush"); !plugin.BoolScalar(plugin.NestedValue(frame, "data", "media_flushed")) {
		t.Fatalf("media scheduler used a different flow: %#v", frame)
	}
	write("shutdown", "shutdown", nil)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("runtime did not shut down")
	}
	for frame := range frames {
		t.Errorf("extra terminal reply: %#v", frame)
	}
	if len(actions.SchedulerRequests) != 3 {
		t.Fatalf("scheduler registrations = %d", len(actions.SchedulerRequests))
	}
	if plugin.StringScalar(actions.SchedulerRequests[0].Payload["action"]) != "check_subscriptions" || plugin.StringScalar(actions.SchedulerRequests[1].Payload["action"]) != "flush_deferred_media" || plugin.StringScalar(actions.SchedulerRequests[2].Payload["action"]) != "check_accounts" {
		t.Fatalf("scheduler payloads = %#v", actions.SchedulerRequests)
	}
	if len(actions.Renders) != 3 {
		t.Fatalf("preview render count = %d", len(actions.Renders))
	}
	for index, template := range []string{"bilibili-update", "weibo-update", "douyin-update"} {
		if actions.Renders[index].Template != template {
			t.Fatalf("preview %d template = %s", index, actions.Renders[index].Template)
		}
	}
	stored, ok := actions.Config["subscriptions"].([]plugin.Subscription)
	if !ok || len(stored) != 1 || stored[0].UID != "42" || !reflect.DeepEqual(stored[0].Services, []string{"artist"}) {
		t.Fatalf("subscription settings changed shape: %#v", actions.Config)
	}
}
