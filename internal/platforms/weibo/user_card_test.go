package weibo

import (
	"bytes"
	"context"
	"html/template"
	"os"
	"strings"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestBuildWeiboUserCardData(t *testing.T) {
	item := plugin.Subscription{
		Platform: "weibo", UID: "6000000001", Name: "测试博主", AvatarURL: "https://tvax2.sinaimg.cn/face.jpg",
		TargetType: "group", TargetID: "100", TargetName: "测试群",
	}
	user := weiboUser{
		UID: "6000000001", Name: "测试博主", AvatarURL: "data:image/png;base64,fixture",
		FansText: "粉丝 379.2万", Verify: "微博认证：知名科技博主", Sign: strings.Repeat("简介", 60),
	}
	data := buildWeiboUserCardData("subscribed", item, user, []string{"post", "image"})
	if data["action"] != "subscribed" || data["platform"] != "微博" || data["subtitle"] != "微博 · 订阅中心" {
		t.Fatalf("unexpected card head: %#v", data)
	}
	if data["uid_text"] != "UID 6000000001" || data["fans_text"] != "粉丝 379.2万" || data["verify_text"] != "微博认证：知名科技博主" || data["verify_org"] != false {
		t.Fatalf("unexpected card identity: %#v", data)
	}
	if data["services_text"] != "微博、图片" {
		t.Fatalf("unexpected services text: %#v", data["services_text"])
	}
	if data["target_text"] != "订阅到：测试群" {
		t.Fatalf("unexpected target text: %#v", data["target_text"])
	}
	cardUser := plugin.MapValue(data["user"])
	if plugin.StringScalar(cardUser["avatar"]) != "data:image/png;base64,fixture" {
		t.Fatalf("card avatar should use the inlined value: %#v", cardUser)
	}
	if sign := plugin.StringScalar(cardUser["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 83 {
		t.Fatalf("sign was not truncated: %d runes", len([]rune(sign)))
	}
}

func TestBuildWeiboUserCardDataUnsubscribed(t *testing.T) {
	item := plugin.Subscription{Platform: "weibo", UID: "6000000001", Name: "测试博主", TargetType: "private", TargetID: "7"}
	data := buildWeiboUserCardData("unsubscribed", item, weiboUser{}, []string{"all"})
	if data["action"] != "unsubscribed" || data["target_text"] != "取消于：私聊" {
		t.Fatalf("unexpected unsubscribe card: %#v", data)
	}
	cardUser := plugin.MapValue(data["user"])
	if plugin.StringScalar(cardUser["name"]) != "测试博主" || plugin.StringScalar(cardUser["uid"]) != "6000000001" {
		t.Fatalf("unsubscribe card lost stored identity: %#v", cardUser)
	}
	if data["fans_text"] != "" || data["verify_text"] != "" || data["services_text"] != "全部" {
		t.Fatalf("empty detail fields should stay empty: %#v", data)
	}
}

func TestInlineWeiboCardAvatar(t *testing.T) {
	fake := testkit.NewActions()
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}
	if got := inlineWeiboCardAvatar(context.Background(), fake, "https://tvax2.sinaimg.cn/crop.0.0.1080.1080.180/face.jpg"); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("allowlisted avatar was not inlined: %q", got)
	}
	for _, sourceURL := range []string{"", "https://example.test/face.jpg", "http://tva1.sinaimg.cn/face.jpg"} {
		if got := inlineWeiboCardAvatar(context.Background(), fake, sourceURL); got != "" {
			t.Fatalf("inlineWeiboCardAvatar(%q) = %q, want empty fallback", sourceURL, got)
		}
	}
	if len(fake.HTTPRequests) != 1 {
		t.Fatalf("unsupported avatars reached http.request: %#v", fake.HTTPRequests)
	}
}

func TestWeiboUserCardTemplatePreservesInlineAvatar(t *testing.T) {
	source, err := os.ReadFile(testkit.RepositoryPath(t, "templates/weibo-user-card/template.html"))
	if err != nil {
		t.Fatalf("read weibo user card template: %v", err)
	}
	compiled, err := template.New("weibo-user-card").Parse(string(source))
	if err != nil {
		t.Fatalf("parse weibo user card template: %v", err)
	}

	const avatar = "data:image/png;base64,fixture"
	data := buildWeiboUserCardData("subscribed",
		plugin.Subscription{Platform: "weibo", UID: "6000000001", Name: "示例博主", TargetType: "group", TargetID: "100", TargetName: "测试群"},
		weiboUser{UID: "6000000001", Name: "示例博主", AvatarURL: avatar, FansText: "粉丝 1.3万", Verify: "微博认证"},
		[]string{"all"})
	data["Stylesheet"] = template.CSS("")
	data["Theme"] = "default"
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute weibo user card template: %v", err)
	}

	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") || !strings.Contains(html, `data-avatar="`+avatar+`"`) {
		t.Fatalf("inline avatar did not survive template escaping: %s", html)
	}
	for _, marker := range []string{"image.src = source", "v-badge", "订阅成功", "微博 · 订阅中心", "assets/weibo-default-avatar.svg"} {
		if !strings.Contains(html, marker) {
			t.Fatalf("weibo user card template missing %q: %s", marker, html)
		}
	}
}
