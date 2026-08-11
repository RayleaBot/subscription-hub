package plugin

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestBuildBilibiliSearchCardData(t *testing.T) {
	users := make([]bilibiliUser, 0, bilibiliSearchResultLimit)
	for index := 1; index <= bilibiliSearchResultLimit; index++ {
		users = append(users, bilibiliUser{UID: strconv.Itoa(index), Name: "示例 UP " + strconv.Itoa(index)})
	}
	users[0].Fans = 1286000
	users[0].Videos = 233
	users[0].Level = 6
	users[0].Senior = true
	users[0].Verify = "bilibili 知名UP主"
	users[0].Live = true
	users[0].Sign = strings.Repeat("简介", 40)

	data := buildBilibiliSearchCardData(" 测试 UP ", users, []string{"*"})
	if data["query"] != "测试 UP" || data["platform"] != "Bilibili" || data["count"] != bilibiliSearchResultLimit {
		t.Fatalf("unexpected card head: %#v", data)
	}
	cards, ok := data["users"].([]map[string]any)
	if !ok || len(cards) != bilibiliSearchResultLimit {
		t.Fatalf("unexpected users payload: %#v", data["users"])
	}
	if cards[0]["rank"] != 1 || cards[9]["rank"] != bilibiliSearchResultLimit {
		t.Fatalf("unexpected ranks: %#v / %#v", cards[0]["rank"], cards[9]["rank"])
	}
	first := cards[0]
	if first["uid_text"] != "UID 1" || first["fans_text"] != "粉丝 128.6万" || first["videos_text"] != "视频 233" {
		t.Fatalf("unexpected first card meta: %#v", first)
	}
	if first["level_icon"] != "assets/lv6-senior.svg" || first["verify_text"] != "bilibili 知名UP主" || first["live"] != true {
		t.Fatalf("unexpected first card badges: %#v", first)
	}
	if sign := stringScalar(first["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 51 {
		t.Fatalf("sign was not truncated: %d runes", len([]rune(sign)))
	}
	if cards[1]["fans_text"] != "" || cards[1]["videos_text"] != "" || cards[1]["verify_text"] != "" || cards[1]["live"] != false {
		t.Fatalf("empty fields should stay empty: %#v", cards[1])
	}
	if cards[1]["level_icon"] != "assets/lv0.svg" {
		t.Fatalf("plain level should map to its badge asset: %#v", cards[1])
	}
	if data["footer_hint"] != "使用前缀 *：*订阅b站推送 [类型] UID或昵称" {
		t.Fatalf("footer should use the active command prefix: %#v", data["footer_hint"])
	}
}

func TestInlineBilibiliSearchAvatarsResolvesConcurrentlyAndDropsRemoteURLs(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	actions := &blockingAvatarActions{
		fakePluginActions: newFakePluginActions(),
		started:           started,
		release:           release,
	}
	users := []bilibiliUser{
		{UID: "1", AvatarURL: "https://i0.hdslb.com/bfs/face/one.webp"},
		{UID: "2", AvatarURL: "https://i1.hdslb.com/bfs/face/two.webp"},
		{UID: "3", AvatarURL: "https://example.test/remote.webp"},
	}

	done := make(chan []bilibiliUser, 1)
	go func() {
		done <- inlineBilibiliSearchAvatars(context.Background(), actions, users)
	}()
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("avatar requests did not start concurrently")
		}
	}
	close(release)
	released = true

	var resolved []bilibiliUser
	select {
	case resolved = <-done:
	case <-time.After(time.Second):
		t.Fatal("avatar resolution did not complete")
	}
	for index := 0; index < 2; index++ {
		if !strings.HasPrefix(resolved[index].AvatarURL, "data:image/png;base64,") {
			t.Fatalf("avatar %d was not inlined: %q", index, resolved[index].AvatarURL)
		}
	}
	if resolved[2].AvatarURL != "" {
		t.Fatalf("unsupported remote avatar reached render data: %q", resolved[2].AvatarURL)
	}
	if users[0].AvatarURL == "" {
		t.Fatal("avatar resolution mutated the search result source")
	}
}

func TestBilibiliSubscribeHintFallsBackWithoutConfiguredPrefix(t *testing.T) {
	if got := bilibiliSubscribeHint([]string{"", "  "}); got != "使用“前缀 + 订阅b站推送 [类型] UID或昵称”" {
		t.Fatalf("unexpected prefix-neutral hint: %q", got)
	}
}

type blockingAvatarActions struct {
	*fakePluginActions
	started chan<- string
	release <-chan struct{}
}

func (actions *blockingAvatarActions) HTTPRequest(ctx context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	select {
	case actions.started <- request.URL:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-actions.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	return rayleabot.ActionResult{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}, nil
}

func TestBilibiliLevelIcon(t *testing.T) {
	for input, want := range map[[2]any]string{
		{0, false}:  "assets/lv0.svg",
		{5, false}:  "assets/lv5.svg",
		{6, false}:  "assets/lv6.svg",
		{6, true}:   "assets/lv6-senior.svg",
		{7, false}:  "",
		{-1, false}: "",
	} {
		if got := bilibiliLevelIcon(input[0].(int), input[1].(bool)); got != want {
			t.Fatalf("bilibiliLevelIcon(%v, %v) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

func TestSearchBilibiliUsersTextFormatsFallback(t *testing.T) {
	users := []bilibiliUser{
		{UID: "1", Name: "甲", Fans: 9999},
		{UID: "2", Name: "乙"},
	}
	lines := strings.Split(searchBilibiliUsersText("测试", users), "\n")
	if len(lines) != 3 || lines[0] != "Bilibili UP 搜索结果：测试" {
		t.Fatalf("unexpected fallback text: %#v", lines)
	}
	if lines[1] != "1. 甲（UID 1）｜粉丝 9999" || lines[2] != "2. 乙（UID 2）" {
		t.Fatalf("unexpected fallback lines: %#v", lines)
	}
}

func TestBilibiliSearchParsesProfileFields(t *testing.T) {
	fake := newFakePluginActions()
	fake.accounts = fixtureAccounts("primary")
	seedSourceState(fake, time.Now(), "", "primary")
	fake.httpResponses = []rayleabot.ActionResult{httpJSONResult(200, map[string]any{
		"code": 0,
		"data": map[string]any{"result": []any{
			map[string]any{
				"mid": 10001, "uname": "测试 UP", "fans": 128000, "videos": 233, "level": 6, "is_live": 1, "is_senior_member": 1,
				"upic": "//i0.hdslb.com/test.jpg", "usign": "测试简介",
				"official_verify": map[string]any{"type": 1, "desc": "bilibili 官方机构认证"},
			},
		}},
	})}
	users, err := searchBilibiliWithActions(context.Background(), fake, "测试 UP")
	if err != nil || len(users) != 1 {
		t.Fatalf("searchBilibiliWithActions() users=%#v, err=%v", users, err)
	}
	user := users[0]
	if user.Videos != 233 || user.Level != 6 || !user.Live || !user.Senior || user.Verify != "bilibili 官方机构认证" || !user.VerifyOrg {
		t.Fatalf("search lost profile fields: %#v", user)
	}
	if len(fake.httpRequests) != 1 || !strings.Contains(fake.httpRequests[0].URL, "pagesize=10") {
		t.Fatalf("search did not request %d results: %#v", bilibiliSearchResultLimit, fake.httpRequests)
	}
}
