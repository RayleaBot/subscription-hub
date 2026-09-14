package weibo

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/httpaction"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

var weiboNumericIDPattern = regexp.MustCompile(`^[0-9]+$`)

var weiboHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

var weiboSearchAnchorPattern = regexp.MustCompile(`(?is)<a\b[^>]+href=["'][^"']*(?:weibo\.com/(?:u/)?|m\.weibo\.cn/u/)([0-9]+)[^"']*["'][^>]*>.*?</a>`)

var weiboSearchNickPattern = regexp.MustCompile(`(?is)\bnick-name=["']([^"']+)["']`)

var weiboSearchTitlePattern = regexp.MustCompile(`(?is)\btitle=["']([^"']+)["']`)

var weiboSearchAltPattern = regexp.MustCompile(`(?is)\balt=["']([^"']+)["']`)

var weiboSearchImagePattern = regexp.MustCompile(`(?is)(?:https?:)?//[^"'\s<>]*sinaimg\.cn[^"'\s<>]+`)

const weiboSearchResultLimit = 8

const weiboCollectMaxDepth = 8

type weiboUser struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	FansText  string `json:"fans_text,omitempty"`
	Sign      string `json:"sign,omitempty"`
	Verify    string `json:"verify,omitempty"`
	VerifyOrg bool   `json:"verify_org,omitempty"`
}

// resolveWeiboUsersWithActions 解析订阅输入：UID/主页链接直接读取资料，昵称走博主搜索。

func resolveWeiboUsersWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]weiboUser, error) {
	query = strings.TrimSpace(query)
	if uid := weiboUIDFromInput(query); uid != "" {
		user, err := readWeiboUserWithActions(ctx, actions, uid)
		if err != nil {
			return nil, err
		}
		return []weiboUser{user}, nil
	}
	return searchWeiboWithActions(ctx, actions, query)
}

func readWeiboUserWithActions(ctx context.Context, actions plugin.SourceActions, uid string) (weiboUser, error) {
	ctx, cancel := context.WithTimeout(ctx, weiboDetailTotalTimeout)
	defer cancel()
	accounts, err := readWeiboAccounts(ctx, actions)
	if err != nil {
		return weiboUser{}, err
	}
	document, err := requestWeiboAcrossAccounts(ctx, actions, accounts, weiboUserContainerURL(uid), weiboMobileReferer)
	if err != nil {
		return weiboUser{}, errors.New(friendlyWeiboSourceError("微博用户信息读取失败", err))
	}
	user := weiboUserFromFlatObject(plugin.MapValue(plugin.NestedValue(document, "data", "userInfo")))
	if user.UID == "" {
		user.UID = uid
	}
	if user.Name == "" {
		return weiboUser{}, errors.New("没有找到这个微博用户")
	}
	return user, nil
}

// matchWeiboUserByQuery 在解析结果中挑选订阅目标：UID/链接输入按 UID 精确匹配，
// 昵称输入要求昵称完全一致；都不满足时返回 nil（调用方提示候选列表）。
func matchWeiboUserByQuery(users []weiboUser, query string) *weiboUser {
	trimmed := strings.TrimSpace(query)
	if uid := weiboUIDFromInput(trimmed); uid != "" {
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

// searchWeiboWithActions 依次尝试 m.weibo.cn 用户搜索（type 3→1），全部为空时兜底 s.weibo.com 网页搜索。
func searchWeiboWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]weiboUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("用法：/微博搜索博主 昵称关键词")
	}
	ctx, cancel := context.WithTimeout(ctx, weiboSearchTotalTimeout)
	defer cancel()
	accounts, err := readWeiboAccounts(ctx, actions)
	if err != nil {
		return nil, err
	}
	var lastError error
	for _, searchType := range []string{"3", "1"} {
		values := url.Values{}
		values.Set("containerid", "100103type="+searchType+"&q="+query)
		values.Set("page_type", "searchall")
		document, requestErr := requestWeiboAcrossAccounts(ctx, actions, accounts, weiboMobileContainerURL+"?"+values.Encode(), weiboMobileReferer)
		if requestErr != nil {
			lastError = requestErr
			continue
		}
		if users := collectWeiboUsers(document["data"]); len(users) > 0 {
			return users, nil
		}
	}
	users, webErr := searchWeiboWebUsers(ctx, actions, accounts, query)
	if webErr == nil && len(users) > 0 {
		return users, nil
	}
	if lastError != nil {
		return nil, errors.New(friendlyWeiboSourceError("微博博主搜索失败", lastError))
	}
	if webErr != nil {
		return nil, errors.New(friendlyWeiboSourceError("微博博主搜索失败", webErr))
	}
	return nil, fmt.Errorf("没有搜索到微博博主：%s", query)
}

// collectWeiboUsers 递归收集搜索响应中的用户对象（card_group/user 等嵌套结构），按 UID 去重。
func collectWeiboUsers(value any) []weiboUser {
	users := make([]weiboUser, 0, weiboSearchResultLimit)
	seen := map[string]bool{}
	collectWeiboUsersInto(value, seen, &users, 0)
	return users
}

func collectWeiboUsersInto(value any, seen map[string]bool, users *[]weiboUser, depth int) {
	if depth > weiboCollectMaxDepth || len(*users) >= weiboSearchResultLimit {
		return
	}
	switch item := value.(type) {
	case map[string]any:
		user := weiboUserFromSearchObject(item)
		if user.UID != "" && user.Name != "" && !seen[user.UID] {
			seen[user.UID] = true
			*users = append(*users, user)
		}
		for _, child := range item {
			collectWeiboUsersInto(child, seen, users, depth+1)
			if len(*users) >= weiboSearchResultLimit {
				return
			}
		}
	case []any:
		for _, child := range item {
			collectWeiboUsersInto(child, seen, users, depth+1)
			if len(*users) >= weiboSearchResultLimit {
				return
			}
		}
	}
}

// weiboUserFromSearchObject 从搜索结果卡片对象提取博主资料，优先读取内嵌 user/userInfo 对象。
func weiboUserFromSearchObject(object map[string]any) weiboUser {
	for _, key := range []string{"user", "userInfo", "profile"} {
		if nested := plugin.MapValue(object[key]); nested != nil {
			if user := weiboUserFromFlatObject(nested); user.UID != "" && user.Name != "" {
				return user
			}
		}
	}
	uid := plugin.FirstText(
		weiboUIDFromInput(plugin.StringScalar(object["scheme"])),
		weiboUIDFromInput(plugin.StringScalar(object["profile_url"])),
		weiboUIDFromInput(plugin.StringScalar(object["url"])),
	)
	if uid == "" && weiboSearchObjectHasUserFields(object) {
		uid = plugin.FirstText(object["uid"], object["id"], object["idstr"])
	}
	if uid == "" {
		return weiboUser{}
	}
	name := cleanWeiboSearchText(plugin.FirstText(object["screen_name"], object["nickname"], object["title_sub"], object["desc"]))
	if name == "" {
		return weiboUser{}
	}
	user := weiboUser{UID: uid, Name: name}
	user.AvatarURL = plugin.NormalizeMediaURL(plugin.FirstNonNil(object["profile_image_url"], object["avatar_large"], object["avatar_hd"], object["avatar"], object["avatar_url"], object["pic"], object["image"]))
	user.FansText = weiboFansText(object["followers_count"], plugin.FirstNonNil(object["followers_count_str"], object["fans"]), descContaining(object["desc1"], "粉丝"))
	user.Verify, user.VerifyOrg = weiboVerifyInfo(object)
	if user.Verify == "" {
		user.Verify = descContaining(object["desc2"], "认证")
	}
	user.Sign = plugin.CleanText(object["description"])
	return user
}

// weiboUserFromFlatObject 解析标准微博用户对象（userInfo / user）。
func weiboUserFromFlatObject(object map[string]any) weiboUser {
	if object == nil {
		return weiboUser{}
	}
	name := cleanWeiboSearchText(plugin.FirstText(object["screen_name"], object["nickname"], object["name"]))
	if name == "" {
		return weiboUser{}
	}
	user := weiboUser{
		UID:       plugin.FirstText(object["id"], object["idstr"], object["uid"]),
		Name:      name,
		AvatarURL: plugin.NormalizeMediaURL(plugin.FirstNonNil(object["profile_image_url"], object["avatar_large"], object["avatar_hd"], object["avatar"], object["avatar_url"])),
		Sign:      plugin.CleanText(object["description"]),
	}
	user.FansText = weiboFansText(object["followers_count"], object["followers_count_str"], "")
	user.Verify, user.VerifyOrg = weiboVerifyInfo(object)
	return user
}

func weiboSearchObjectHasUserFields(object map[string]any) bool {
	for _, key := range []string{"screen_name", "nickname", "avatar_hd", "avatar_large", "profile_image_url"} {
		if strings.TrimSpace(plugin.StringScalar(object[key])) != "" {
			return true
		}
	}
	return false
}

// weiboFansText 生成粉丝数展示文案：优先数字字段，其次“粉丝 xxx”类描述文本。
func weiboFansText(followersCount, followersText any, desc string) string {
	if count := plugin.IntScalar(followersCount); count > 0 {
		return "粉丝 " + plugin.FormatCount(int(count))
	}
	for _, candidate := range []string{plugin.CleanText(followersText), cleanWeiboSearchText(desc)} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "粉丝") {
			return candidate
		}
		return "粉丝 " + strings.TrimLeft(candidate, " ：:")
	}
	return ""
}

// weiboVerifyInfo 解析认证信息：verified_type 0 为个人橙 V，大于 0 为机构蓝 V。
func weiboVerifyInfo(object map[string]any) (string, bool) {
	verifiedType := plugin.IntScalar(object["verified_type"])
	if !plugin.BoolScalar(object["verified"]) && verifiedType <= 0 {
		return "", false
	}
	reason := plugin.CleanText(object["verified_reason"])
	if reason == "" {
		reason = "微博认证"
	}
	return reason, verifiedType > 0
}

func descContaining(value any, marker string) string {
	text := cleanWeiboSearchText(plugin.StringScalar(value))
	if text == "" || !strings.Contains(text, marker) {
		return ""
	}
	return text
}

// cleanWeiboSearchText 清洗搜索结果中的昵称/描述文本：去除高亮标签、转义与前后缀噪音。
func cleanWeiboSearchText(value string) string {
	text := html.UnescapeString(strings.TrimSpace(value))
	for range 2 {
		decoded, err := url.QueryUnescape(text)
		if err != nil || decoded == text {
			break
		}
		text = decoded
	}
	text = weiboHTMLTagPattern.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimSpace(strings.TrimPrefix(text, "@"))
	text = strings.TrimSuffix(text, "的微博主页")
	text = strings.TrimSuffix(text, "的微博")
	if !weiboSearchNameUsable(text) {
		return ""
	}
	return text
}

func weiboSearchNameUsable(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" || len([]rune(text)) > 48 {
		return false
	}
	lower := strings.ToLower(text)
	if strings.ContainsAny(text, "<>=\"") || strings.Contains(text, "%") {
		return false
	}
	for _, marker := range []string{"click:user_name", "seqid:", "ext:mpos", "suda-data", "woo-button", "target=_blank", "class="} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

// searchWeiboWebUsers 兜底抓取 s.weibo.com 用户搜索页并解析博主条目。
func searchWeiboWebUsers(ctx context.Context, actions plugin.SourceActions, accounts []weiboAccount, query string) ([]weiboUser, error) {
	rawURL := weiboWebUserSearchURL + "?" + url.Values{"q": []string{strings.TrimSpace(query)}}.Encode()
	var lastError error
	for _, account := range accounts {
		response, err := actions.HTTPRequest(ctx, httpaction.Request{
			Method: "GET", URL: rawURL, Headers: weiboSearchPageHeaders(account.Cookie), TimeoutSeconds: weiboRequestTimeoutSeconds,
		})
		if err != nil {
			lastError = err
			continue
		}
		status := int(plugin.IntScalar(response["status_code"]))
		body, _ := response["body_text"].(string)
		if status != 200 || body == "" {
			lastError = &weiboSourceError{Kind: weiboErrorKind(status), HTTPStatus: status}
			continue
		}
		if users := weiboUsersFromSearchPage(body); len(users) > 0 {
			return users, nil
		}
		// 页面正常返回但没有可用结果：不算失败，交给上层报“没有搜索到”。
		return nil, nil
	}
	if lastError == nil {
		lastError = errors.New("微博网页搜索没有返回可用结果")
	}
	return nil, lastError
}

func weiboUsersFromSearchPage(body string) []weiboUser {
	matches := weiboSearchAnchorPattern.FindAllStringSubmatchIndex(body, -1)
	users := make([]weiboUser, 0, min(len(matches), weiboSearchResultLimit))
	seen := map[string]bool{}
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		uid := body[match[2]:match[3]]
		if seen[uid] {
			continue
		}
		anchor := body[match[0]:match[1]]
		start := max(0, match[0]-800)
		end := min(len(body), match[1]+800)
		chunk := body[start:end]
		name := weiboSearchNameFromHTML(anchor)
		if name == "" {
			continue
		}
		seen[uid] = true
		users = append(users, weiboUser{UID: uid, Name: name, AvatarURL: weiboSearchAvatarFromHTML(chunk)})
		if len(users) >= weiboSearchResultLimit {
			break
		}
	}
	return users
}

func weiboSearchNameFromHTML(value string) string {
	for _, pattern := range []*regexp.Regexp{weiboSearchNickPattern, weiboSearchTitlePattern, weiboSearchAltPattern} {
		if match := pattern.FindStringSubmatch(value); len(match) > 1 {
			if name := cleanWeiboSearchText(match[1]); name != "" {
				return name
			}
		}
	}
	start := strings.Index(value, ">")
	end := strings.LastIndex(strings.ToLower(value), "</a>")
	if start >= 0 && end > start {
		return cleanWeiboSearchText(value[start+1 : end])
	}
	return ""
}

func weiboSearchAvatarFromHTML(value string) string {
	if match := weiboSearchImagePattern.FindString(value); match != "" {
		return plugin.NormalizeMediaURL(html.UnescapeString(match))
	}
	return ""
}

// weiboUIDFromInput 识别纯数字 UID 或微博主页链接中的 UID；普通昵称返回空。
func weiboUIDFromInput(query string) string {
	text := strings.TrimSpace(query)
	if weiboNumericIDPattern.MatchString(text) {
		return text
	}
	for _, raw := range plugin.URLPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host != "weibo.com" && !strings.HasSuffix(host, ".weibo.com") && host != "weibo.cn" && !strings.HasSuffix(host, ".weibo.cn") {
			continue
		}
		values := parsed.Query()
		for _, key := range []string{"uid", "value"} {
			if candidate := strings.TrimSpace(values.Get(key)); weiboNumericIDPattern.MatchString(candidate) {
				return candidate
			}
		}
		parts := plugin.PathParts(parsed.Path)
		for index, part := range parts {
			if part == "u" || part == "profile" {
				if index+1 < len(parts) && weiboNumericIDPattern.MatchString(parts[index+1]) {
					return parts[index+1]
				}
				continue
			}
			if weiboNumericIDPattern.MatchString(part) {
				return part
			}
		}
	}
	return ""
}
