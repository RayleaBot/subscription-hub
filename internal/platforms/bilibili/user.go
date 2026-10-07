package bilibili

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

type bilibiliUser struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Fans      int    `json:"fans,omitempty"`
	Sign      string `json:"sign,omitempty"`
	Videos    int    `json:"videos,omitempty"`
	Level     int    `json:"level,omitempty"`
	Verify    string `json:"verify,omitempty"`
	VerifyOrg bool   `json:"verify_org,omitempty"`
	Live      bool   `json:"live,omitempty"`
	Senior    bool   `json:"senior,omitempty"`
}

func resolveBilibiliUsersWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]bilibiliUser, error) {
	query = strings.TrimSpace(query)
	if uid := subjectIDFromInput(query); uid != "" {
		user, err := readBilibiliUserWithActions(ctx, actions, uid)
		if err != nil {
			return nil, err
		}
		return []bilibiliUser{user}, nil
	}
	return searchBilibiliWithActions(ctx, actions, query)
}

// matchBilibiliUserByQuery 在解析结果中挑选订阅目标：UID/链接输入按 UID 精确匹配，
// 昵称输入要求昵称完全一致；都不满足时返回 nil（调用方提示候选列表）。
func matchBilibiliUserByQuery(users []bilibiliUser, query string) *bilibiliUser {
	trimmed := strings.TrimSpace(query)
	if uid := subjectIDFromInput(trimmed); uid != "" {
		for index := range users {
			if users[index].UID == uid {
				return &users[index]
			}
		}
	}
	for index := range users {
		if strings.EqualFold(strings.TrimSpace(users[index].Name), trimmed) {
			return &users[index]
		}
	}
	return nil
}

const bilibiliUserDetailTimeout = 15 * time.Second

func readBilibiliUserWithActions(ctx context.Context, actions plugin.SourceActions, uid string) (bilibiliUser, error) {
	ctx, cancel := context.WithTimeout(ctx, bilibiliUserDetailTimeout)
	defer cancel()
	accounts, err := readBilibiliAccounts(ctx, actions)
	if err != nil {
		return bilibiliUser{}, err
	}
	values := bilibiliDeviceQuery()
	values.Set("mid", uid)
	values.Set("platform", "web")
	values.Set("web_location", "1550101")
	endpoint := bilibiliUserInfoURL + "?" + values.Encode()
	document, err := requestBilibiliAcrossAccounts(ctx, actions, accounts, "GET", endpoint, true, false)
	if err != nil {
		return bilibiliUser{}, errors.New(friendlyBilibiliSourceError("Bilibili 用户信息读取失败", err))
	}
	data := plugin.MapValue(document["data"])
	name := plugin.CleanText(plugin.FirstNonNil(data["name"], data["uname"]))
	resolvedUID := plugin.FirstText(data["mid"], uid)
	if plugin.Digits(resolvedUID) == "" || name == "" {
		return bilibiliUser{}, errors.New("没有找到这个 Bilibili 用户")
	}
	user := bilibiliUser{
		UID:       resolvedUID,
		Name:      name,
		AvatarURL: plugin.NormalizeMediaURL(plugin.FirstNonNil(data["face"], data["avatar"], data["upic"])),
		Sign:      plugin.CleanText(data["sign"]),
		Level:     int(plugin.IntScalar(data["level"])),
	}
	if verify := plugin.MapValue(data["official"]); verify != nil {
		user.Verify = firstCleanText(verify["title"], verify["desc"])
		user.VerifyOrg = plugin.IntScalar(verify["type"]) == 1
	}
	// 粉丝数与投稿数是补充信息，并发读取并各给 8s 预算，避免串行放大整体耗时。
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		user.Fans = readBilibiliFans(ctx, actions, accounts[0], resolvedUID)
	}()
	go func() {
		defer wait.Done()
		user.Videos = readBilibiliVideos(ctx, actions, accounts[0], resolvedUID)
	}()
	wait.Wait()
	return user, nil
}

// readBilibiliFans 读取 UP 主粉丝数，失败时静默返回 0（卡片不显示粉丝行）。
func readBilibiliFans(ctx context.Context, actions plugin.SourceActions, account bilibiliAccount, uid string) int {
	endpoint := bilibiliRelationStatURL + "?" + url.Values{"vmid": []string{uid}}.Encode()
	client := newBilibiliClient(actions)
	client.timeoutSeconds = 8
	document, err := client.requestJSON(ctx, "GET", endpoint, account, false, false, "", false)
	if err != nil {
		return 0
	}
	return int(plugin.IntScalar(plugin.NestedValue(document, "data", "follower")))
}

// readBilibiliVideos 读取 UP 主投稿视频数，失败时静默返回 0（卡片不显示视频行）。
func readBilibiliVideos(ctx context.Context, actions plugin.SourceActions, account bilibiliAccount, uid string) int {
	values := bilibiliDeviceQuery()
	values.Set("mid", uid)
	values.Set("ps", "1")
	values.Set("pn", "1")
	values.Set("order", "pubdate")
	endpoint := bilibiliUserVideosURL + "?" + values.Encode()
	client := newBilibiliClient(actions)
	client.timeoutSeconds = 8
	document, err := client.requestJSON(ctx, "GET", endpoint, account, true, false, "", true)
	if err != nil {
		return 0
	}
	return int(plugin.IntScalar(plugin.NestedValue(document, "data", "page", "count")))
}

const bilibiliSearchResultLimit = 10

const bilibiliSearchTotalTimeout = 15 * time.Second

func searchBilibiliWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]bilibiliUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("用法：/b站搜索up UP昵称关键词")
	}
	ctx, cancel := context.WithTimeout(ctx, bilibiliSearchTotalTimeout)
	defer cancel()
	accounts, err := readBilibiliAccounts(ctx, actions)
	if err != nil {
		return nil, err
	}
	values := bilibiliDeviceQuery()
	values.Set("search_type", "bili_user")
	values.Set("order", "totalrank")
	values.Set("page", "1")
	values.Set("pagesize", strconv.Itoa(bilibiliSearchResultLimit))
	values.Set("keyword", query)
	values.Set("web_location", "1430654")
	endpoint := bilibiliUserSearchURL + "?" + values.Encode()
	document, err := requestBilibiliAcrossAccounts(ctx, actions, accounts, "GET", endpoint, true, false)
	if err != nil {
		return nil, errors.New(friendlyBilibiliSourceError("Bilibili UP 搜索失败", err))
	}
	results := plugin.SliceValue(plugin.NestedValue(document, "data", "result"))
	users := make([]bilibiliUser, 0, len(results))
	for _, raw := range results {
		item := plugin.MapValue(raw)
		uid := plugin.StringScalar(item["mid"])
		name := plugin.CleanText(plugin.HTMLTag.ReplaceAllString(plugin.FirstText(item["uname"], item["name"]), ""))
		if uid != "" && name != "" {
			verify, verifyOrg := bilibiliVerifyBadge(item["official_verify"])
			users = append(users, bilibiliUser{
				UID:       uid,
				Name:      name,
				AvatarURL: plugin.NormalizeMediaURL(plugin.FirstNonNil(item["upic"], item["face"], item["avatar"])),
				Fans:      int(plugin.IntScalar(item["fans"])),
				Sign:      plugin.CleanText(plugin.FirstNonNil(item["usign"], item["sign"])),
				Videos:    int(plugin.IntScalar(item["videos"])),
				Level:     int(plugin.IntScalar(item["level"])),
				Verify:    verify,
				VerifyOrg: verifyOrg,
				Live:      plugin.IntScalar(item["is_live"]) == 1,
				Senior:    plugin.IntScalar(item["is_senior_member"]) == 1,
			})
			if len(users) == bilibiliSearchResultLimit {
				break
			}
		}
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("没有搜索到 Bilibili 用户：%s", query)
	}
	return users, nil
}

// bilibiliVerifyBadge 解析搜索结果的官方认证信息，返回认证文案与是否机构认证。
func bilibiliVerifyBadge(value any) (string, bool) {
	verify := plugin.MapValue(value)
	if verify == nil {
		return "", false
	}
	return plugin.CleanText(verify["desc"]), plugin.IntScalar(verify["type"]) == 1
}

func friendlyBilibiliError(err error) string {
	if err == nil {
		return "没有找到匹配的 Bilibili UP 主。"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Bilibili 用户信息读取失败。"
	}
	if !strings.HasSuffix(message, "。") {
		message += "。"
	}
	return message
}
