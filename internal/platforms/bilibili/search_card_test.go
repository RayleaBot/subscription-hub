package bilibili

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/httpaction"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
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
	if sign := plugin.StringScalar(first["sign"]); !strings.HasSuffix(sign, "...") || len([]rune(sign)) > 51 {
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

func TestInlineBilibiliSearchAvatarsFetchesAllThumbnailsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	actions := &blockingAvatarActions{
		Actions: newActions(),
		started: started,
		release: release,
	}
	users := []bilibiliUser{
		{UID: "1", AvatarURL: "https://i0.hdslb.com/bfs/face/one.webp"},
		{UID: "2", AvatarURL: "https://i1.hdslb.com/bfs/face/two.webp"},
	}

	type result struct {
		users []bilibiliUser
	}
	done := make(chan result, 1)
	go func() {
		resolved := prepareBilibiliSearchAvatars(context.Background(), actions, users)
		done <- result{users: resolved}
	}()
	requested := make([]string, 0, 2)
	for index := 0; index < 2; index++ {
		select {
		case requestURL := <-started:
			requested = append(requested, requestURL)
		case <-time.After(time.Second):
			t.Fatal("avatar requests did not start concurrently")
		}
	}
	close(release)
	released = true

	var prepared result
	select {
	case prepared = <-done:
	case <-time.After(time.Second):
		t.Fatal("avatar resolution did not complete")
	}
	for index := 0; index < 2; index++ {
		if !strings.HasPrefix(prepared.users[index].AvatarURL, "data:image/png;base64,") {
			t.Fatalf("avatar %d was not inlined: %q", index, prepared.users[index].AvatarURL)
		}
	}
	for _, requestURL := range requested {
		if !strings.HasSuffix(requestURL, bilibiliSearchAvatarSuffix) {
			t.Fatalf("search avatar did not request a compact thumbnail: %q", requestURL)
		}
	}
	if users[0].AvatarURL == "" {
		t.Fatal("avatar resolution mutated the search result source")
	}
}

func TestInlineBilibiliSearchAvatarsRetriesOriginalSource(t *testing.T) {
	sourceURL := "https://i2.hdslb.com/bfs/face/fallback.webp"
	actions := &fallbackAvatarActions{Actions: newActions()}
	resolved := prepareBilibiliSearchAvatars(context.Background(), actions, []bilibiliUser{{UID: "1", AvatarURL: sourceURL}})
	if len(resolved) != 1 || !strings.HasPrefix(resolved[0].AvatarURL, "data:image/png;base64,") {
		t.Fatalf("original avatar retry failed: users=%#v", resolved)
	}
	if len(actions.requests) != 2 || !strings.HasSuffix(actions.requests[0].URL, bilibiliSearchAvatarSuffix) || actions.requests[1].URL != sourceURL {
		t.Fatalf("unexpected avatar retry requests: %#v", actions.requests)
	}
	for _, request := range actions.requests {
		if request.TimeoutSeconds != bilibiliSearchAvatarTimeoutSeconds {
			t.Fatalf("avatar timeout = %d, want %d", request.TimeoutSeconds, bilibiliSearchAvatarTimeoutSeconds)
		}
	}
}

func TestInlineBilibiliSearchAvatarsUsesDefaultAvatarWithoutThumbnailSuffix(t *testing.T) {
	sourceURL := "https://static.hdslb.com/images/member/noface.gif"
	actions := newActions()

	resolved := prepareBilibiliSearchAvatars(context.Background(), actions, []bilibiliUser{{UID: "1", AvatarURL: sourceURL}})
	if len(resolved) != 1 || resolved[0].AvatarURL != bilibiliSearchFallbackAvatar {
		t.Fatalf("default search avatar did not use the template asset: %#v", resolved)
	}
	if len(actions.HTTPRequests) != 0 {
		t.Fatalf("default avatar should not require a remote request: %#v", actions.HTTPRequests)
	}
}

func TestInlineBilibiliSearchAvatarsUsesFallbackForIncompleteResults(t *testing.T) {
	sourceURL := "https://i2.hdslb.com/bfs/face/fallback.webp"
	actions := &testkit.FailedAvatarActions{Actions: newActions()}
	resolved := prepareBilibiliSearchAvatars(context.Background(), actions, []bilibiliUser{{UID: "1", AvatarURL: sourceURL}})
	if len(resolved) != 1 || resolved[0].AvatarURL != bilibiliSearchFallbackAvatar {
		t.Fatalf("incomplete avatar did not use the template asset: %#v", resolved)
	}
	if len(actions.Requests) != 2 {
		t.Fatalf("avatar attempts = %d, want 2", len(actions.Requests))
	}
}

func TestInlineBilibiliSearchAvatarsSupportsGarbAvatar(t *testing.T) {
	sourceURL := "https://i1.hdslb.com/bfs/garb/avatar.png"
	actions := newActions()
	actions.HTTPResponses = []rayleabot.ActionResult{testkit.AvatarHTTPResult()}

	resolved := prepareBilibiliSearchAvatars(context.Background(), actions, []bilibiliUser{{UID: "1", AvatarURL: sourceURL}})
	if len(resolved) != 1 || !strings.HasPrefix(resolved[0].AvatarURL, "data:image/png;base64,") {
		t.Fatalf("garb avatar was not inlined: %#v", resolved)
	}
	if len(actions.HTTPRequests) != 1 || !strings.HasSuffix(actions.HTTPRequests[0].URL, bilibiliSearchAvatarSuffix) {
		t.Fatalf("garb avatar did not request a compact thumbnail: %#v", actions.HTTPRequests)
	}
}

func TestBilibiliSubscribeHintFallsBackWithoutConfiguredPrefix(t *testing.T) {
	if got := bilibiliSubscribeHint([]string{"", "  "}); got != "使用“前缀 + 订阅b站推送 [类型] UID或昵称”" {
		t.Fatalf("unexpected prefix-neutral hint: %q", got)
	}
}

type blockingAvatarActions struct {
	*testkit.Actions
	started chan<- string
	release <-chan struct{}
}

type fallbackAvatarActions struct {
	*testkit.Actions
	requests []httpaction.Request
}

func (actions *fallbackAvatarActions) HTTPRequest(_ context.Context, request httpaction.Request) (rayleabot.ActionResult, error) {
	actions.requests = append(actions.requests, request)
	if len(actions.requests) == 1 {
		return nil, context.DeadlineExceeded
	}
	return testkit.AvatarHTTPResult(), nil
}

func (actions *blockingAvatarActions) HTTPRequest(ctx context.Context, request httpaction.Request) (rayleabot.ActionResult, error) {
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
	return testkit.AvatarHTTPResult(), nil
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

func TestBilibiliSearchParsesProfileFields(t *testing.T) {
	fake := newActions()
	fake.SeedAccounts("bilibili", fixtureAccounts("primary"))
	seedSourceState(fake, time.Now(), "", "primary")
	fake.HTTPResponses = []rayleabot.ActionResult{testkit.HTTPJSON(200, map[string]any{
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
	if len(fake.HTTPRequests) != 1 || !strings.Contains(fake.HTTPRequests[0].URL, "pagesize=10") {
		t.Fatalf("search did not request %d results: %#v", bilibiliSearchResultLimit, fake.HTTPRequests)
	}
}
