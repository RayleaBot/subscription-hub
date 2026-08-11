package plugin

import (
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestBuildBilibiliUserCardData(t *testing.T) {
	item := subscription{
		Platform: "bilibili", UID: "42", Name: "测试 UP", AvatarURL: "https://i0.hdslb.com/face.jpg",
		TargetType: "group", TargetID: "100", TargetName: "测试群",
	}
	user := bilibiliUser{UID: "42", Name: "测试 UP", Fans: 1286000, Sign: strings.Repeat("简介", 60)}
	data := buildBilibiliUserCardData("subscribed", item, user, []string{"live", "video"})
	if data["action"] != "subscribed" || data["platform"] != "Bilibili" {
		t.Fatalf("unexpected card head: %#v", data)
	}
	if data["uid_text"] != "UID 42" || data["fans_text"] != "粉丝 128.6万" {
		t.Fatalf("unexpected card identity: %#v", data)
	}
	if data["services_text"] != "直播、视频" {
		t.Fatalf("unexpected services text: %#v", data["services_text"])
	}
	labels, ok := data["service_labels"].([]string)
	if !ok || len(labels) != 2 || labels[0] != "直播" || labels[1] != "视频" {
		t.Fatalf("unexpected service labels: %#v", data["service_labels"])
	}
	if data["target_text"] != "订阅到：测试群" {
		t.Fatalf("unexpected target text: %#v", data["target_text"])
	}
	cardUser := mapValue(data["user"])
	if stringScalar(cardUser["avatar"]) != "https://i0.hdslb.com/face.jpg" {
		t.Fatalf("avatar should fall back to stored value: %#v", cardUser)
	}
	if sign := stringScalar(cardUser["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 83 {
		t.Fatalf("sign was not truncated: %d runes", len([]rune(sign)))
	}
}

func TestBuildBilibiliUserCardDataUnsubscribed(t *testing.T) {
	item := subscription{Platform: "bilibili", UID: "42", Name: "测试 UP", TargetType: "private", TargetID: "7"}
	data := buildBilibiliUserCardData("unsubscribed", item, bilibiliUser{}, []string{"all"})
	if data["action"] != "unsubscribed" || data["target_text"] != "取消于：私聊" {
		t.Fatalf("unexpected unsubscribe card: %#v", data)
	}
	if data["fans_text"] != "" {
		t.Fatalf("fans text should be hidden without fans: %#v", data["fans_text"])
	}
	if data["services_text"] != "全部" {
		t.Fatalf("unexpected services text: %#v", data["services_text"])
	}
}

func TestFansTextFormatting(t *testing.T) {
	for input, want := range map[int]string{0: "", -3: "", 9999: "粉丝 9999", 10000: "粉丝 1.0万", 1286000: "粉丝 128.6万"} {
		if got := fansText(input); got != want {
			t.Fatalf("fansText(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestRemoveSubscriptionOutcomeCarriesCardData(t *testing.T) {
	current := settings{Subscriptions: []subscription{{
		ID: "one", Platform: "bilibili", UID: "42", Name: "测试 UP", AvatarURL: "https://i0.hdslb.com/face.jpg",
		TargetType: "group", TargetID: "100", TargetName: "测试群", Services: []string{"all"}, Enabled: true,
	}}}
	event := &rayleabot.EventContext{Event: rayleabot.Event{
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": []string{"直播", "42"}},
	}}
	outcome := removeSubscription(&current, event, "bilibili")
	if !outcome.Changed || outcome.Action != "unsubscribed" || outcome.Message != "已取消订阅：Bilibili 42（直播）" {
		t.Fatalf("unexpected remove outcome: %#v", outcome)
	}
	if outcome.Item == nil || outcome.Item.UID != "42" || outcome.Item.Name != "测试 UP" || outcome.Item.AvatarURL == "" {
		t.Fatalf("remove outcome lost card profile: %#v", outcome.Item)
	}
	if len(outcome.Services) != 1 || outcome.Services[0] != "live" {
		t.Fatalf("remove outcome lost removed services: %#v", outcome.Services)
	}
}
