package douyin

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const douyinSearchResultLimit = 10

const douyinSearchPauseKey = "source:douyin:search:pause"

const douyinSearchEmptyPause = 5 * time.Minute

const douyinSearchVerificationPause = 30 * time.Minute

type douyinUser struct {
	UID       string `json:"uid"`
	UniqueID  string `json:"unique_id,omitempty"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	FansText  string `json:"fans_text,omitempty"`
	Sign      string `json:"sign,omitempty"`
	Verify    string `json:"verify,omitempty"`
	VerifyOrg bool   `json:"verify_org,omitempty"`
}

func resolveDouyinUsersWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]douyinUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("请填写用户主页链接、完整 sec_uid 或昵称关键词")
	}
	if secUID := douyinSecUIDFromInput(query); secUID != "" {
		user, err := readDouyinUserWithActions(ctx, actions, secUID)
		if err != nil {
			return nil, err
		}
		return []douyinUser{user}, nil
	}
	if contentURL := douyinContentURLFromInput(query); contentURL != "" {
		user, err := readDouyinAuthorFromURL(ctx, actions, contentURL)
		if err != nil {
			return nil, err
		}
		return []douyinUser{user}, nil
	}
	return searchDouyinWithActions(ctx, actions, query)
}

func readDouyinUserWithActions(ctx context.Context, actions plugin.SourceActions, secUID string) (douyinUser, error) {
	ctx, cancel := context.WithTimeout(ctx, douyinDetailTotalTimeout)
	defer cancel()
	accounts, err := readDouyinAccounts(ctx, actions)
	if err != nil {
		return douyinUser{}, err
	}
	user, jsonErr := readDouyinUserFromJSON(ctx, actions, accounts, secUID)
	if jsonErr == nil && user.UID != "" && user.Name != "" {
		return user, nil
	}
	body, htmlErr := requestDouyinHTMLAcrossAccounts(ctx, actions, accounts, douyinUserPageURL(secUID), douyinWebReferer)
	if htmlErr != nil {
		if jsonErr != nil {
			return douyinUser{}, errors.New(friendlyDouyinSourceError("抖音用户信息读取失败", jsonErr))
		}
		return douyinUser{}, errors.New(friendlyDouyinSourceError("抖音用户信息读取失败", htmlErr))
	}
	user = douyinUserFromPage(body)
	if user.UID == "" {
		user.UID = secUID
	}
	if user.Name == "" {
		if jsonErr != nil {
			return douyinUser{}, errors.New(friendlyDouyinSourceError("抖音用户信息读取失败", jsonErr))
		}
		return douyinUser{}, errors.New("没有找到这个抖音用户")
	}
	return user, nil
}

func readDouyinUserFromJSON(ctx context.Context, actions plugin.SourceActions, accounts []douyinAccount, secUID string) (douyinUser, error) {
	document, err := requestDouyinJSONAcrossAccounts(ctx, actions, accounts, douyinProfileAPIURL(secUID), douyinUserPageURL(secUID))
	if err != nil {
		return douyinUser{}, err
	}
	user := douyinUserFromValue(document)
	if user.UID == "" {
		user.UID = secUID
	}
	if user.Name == "" {
		return douyinUser{}, errors.New("没有找到这个抖音用户")
	}
	return user, nil
}

func readDouyinAuthorFromURL(ctx context.Context, actions plugin.SourceActions, rawURL string) (douyinUser, error) {
	ctx, cancel := context.WithTimeout(ctx, douyinDetailTotalTimeout)
	defer cancel()
	accounts, err := readDouyinAccounts(ctx, actions)
	if err != nil {
		return douyinUser{}, err
	}
	body, err := requestDouyinHTMLAcrossAccounts(ctx, actions, accounts, rawURL, douyinWebReferer)
	if err != nil {
		return douyinUser{}, errors.New(friendlyDouyinSourceError("抖音链接解析失败", err))
	}
	user := douyinUserFromPage(body)
	if user.UID == "" || user.Name == "" {
		return douyinUser{}, errors.New("没有从这条抖音链接解析到作者")
	}
	return user, nil
}

func searchDouyinWithActions(ctx context.Context, actions plugin.SourceActions, query string) ([]douyinUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("用法：/抖音搜索用户 昵称关键词")
	}
	if remaining, reason := douyinSearchPauseRemaining(ctx, actions, time.Now()); remaining > 0 {
		// 暂停期间不再发起纯 HTTP 探测（避免继续累积风控），但宿主浏览器
		// resolve 与 HTTP 通道相互独立：浏览器内请求不增加 HTTP 探测次数，
		// 尝试一次，成功即清除暂停。
		if accounts, accountErr := readDouyinAccounts(ctx, actions); accountErr == nil && len(accounts) > 0 {
			resolveCookie := ""
			for _, account := range accounts {
				if strings.TrimSpace(account.Cookie) != "" {
					resolveCookie = account.Cookie
					break
				}
			}
			if resolveCookie != "" {
				if resolved, resolveErr := searchDouyinViaBrowser(ctx, actions, query, resolveCookie); resolveErr == nil && len(resolved) > 0 {
					clearDouyinSearchPause(ctx, actions)
					return resolved, nil
				}
			}
		}
		return nil, errors.New(friendlyDouyinSourceError("抖音用户搜索失败", &douyinSourceError{
			Kind: "search_paused", StatusMsg: reason, RetryAfter: remaining,
		}))
	}
	searchCtx, cancel := context.WithTimeout(ctx, douyinSearchTotalTimeout)
	defer cancel()
	accounts, err := readDouyinAccounts(searchCtx, actions)
	if err != nil {
		return nil, err
	}
	users, jsonErr := searchDouyinFromJSON(searchCtx, actions, accounts, query)
	if len(users) > 0 {
		clearDouyinSearchPause(ctx, actions)
		return users, nil
	}
	var htmlErr error
	if douyinHTMLFallbackAllowed(jsonErr) {
		body, err := requestDouyinHTMLAcrossAccounts(searchCtx, actions, accounts, douyinSearchPageURL(query), douyinWebReferer)
		htmlErr = err
		if err == nil {
			if users = douyinUsersFromSearchPage(body, query); len(users) > 0 {
				clearDouyinSearchPause(ctx, actions)
				return users, nil
			}
		}
	}
	if len(accounts) > 0 {
		resolveCookie := ""
		for _, account := range accounts {
			if strings.TrimSpace(account.Cookie) != "" {
				resolveCookie = account.Cookie
				break
			}
		}
		if resolved, resolveErr := searchDouyinViaBrowser(ctx, actions, query, resolveCookie); resolveErr == nil && len(resolved) > 0 {
			clearDouyinSearchPause(ctx, actions)
			return resolved, nil
		} else if resolveErr != nil {
			logDouyinFailure(ctx, actions, "抖音用户查找失败", resolveErr)
		}
	}
	searchErr := jsonErr
	if htmlErr != nil && (searchErr == nil || douyinSearchPauseDuration(htmlErr) > 0) {
		searchErr = htmlErr
	}
	if searchErr == nil {
		endpoint := douyinSearchAPIURLs(query)[0]
		searchErr = &douyinSourceError{
			Kind: "search_unavailable", HTTPStatus: 200, Endpoint: douyinEndpointPath(endpoint), RetryAfter: douyinSearchEmptyPause,
		}
	}
	if delay := douyinSearchPauseDuration(searchErr); delay > 0 {
		rememberDouyinSearchPause(ctx, actions, searchErr, delay, time.Now())
	}
	logDouyinFailure(ctx, actions, "抖音用户搜索失败", searchErr)
	return nil, errors.New(friendlyDouyinSourceError("抖音用户搜索失败", searchErr))
}

// douyinHTMLFallbackAllowed 判断 JSON 端点失败后是否值得尝试 HTML 搜索页回退。
// 账号被风控/限流/会话拦截/鉴权拒绝时，无签名 HTML 请求同样会撞验证页，
// 跳过回退避免无谓请求继续累积风控风险。
func douyinHTMLFallbackAllowed(jsonErr error) bool {
	var sourceErr *douyinSourceError
	if !errors.As(jsonErr, &sourceErr) {
		return true
	}
	switch sourceErr.Kind {
	case "risk_control", "rate_limit", "session_blocked", "auth":
		return false
	default:
		return true
	}
}

func douyinSearchPauseRemaining(ctx context.Context, actions plugin.SourceActions, now time.Time) (time.Duration, string) {
	if actions == nil {
		return 0, ""
	}
	result, err := actions.KVGet(ctx, douyinSearchPauseKey)
	if err != nil {
		return 0, ""
	}
	stored, exists := plugin.ActionStoredValue(result)
	if !exists {
		return 0, ""
	}
	state := plugin.MapValue(stored)
	remaining := time.Unix(plugin.IntScalar(state["until"]), 0).Sub(now)
	if remaining <= 0 {
		return 0, ""
	}
	return remaining, plugin.StringScalar(state["kind"])
}

func rememberDouyinSearchPause(ctx context.Context, actions plugin.SourceActions, err error, delay time.Duration, now time.Time) {
	if actions == nil || delay <= 0 {
		return
	}
	kind := "upstream"
	var sourceErr *douyinSourceError
	if errors.As(err, &sourceErr) {
		kind = plugin.FirstText(sourceErr.Kind, kind)
	}
	_, _ = actions.KVSet(ctx, douyinSearchPauseKey, map[string]any{
		"kind": kind, "until": now.Add(delay).Unix(),
	})
}

func clearDouyinSearchPause(ctx context.Context, actions plugin.SourceActions) {
	if actions == nil {
		return
	}
	if result, err := actions.KVGet(ctx, douyinSearchPauseKey); err == nil {
		if _, exists := plugin.ActionStoredValue(result); exists {
			_, _ = actions.KVDelete(ctx, douyinSearchPauseKey)
		}
	}
}

func douyinSearchPauseDuration(err error) time.Duration {
	var sourceErr *douyinSourceError
	if !errors.As(err, &sourceErr) {
		return 0
	}
	switch sourceErr.Kind {
	case "risk_control", "rate_limit", "session_blocked":
		return douyinSearchVerificationPause
	case "search_unavailable":
		return douyinSearchEmptyPause
	default:
		return 0
	}
}

// searchDouyinFromJSON 依次尝试多个搜索端点：任一返回有效用户即成功，
// 全部失败时返回首个错误用于诊断。空成功响应记录结构摘要（不含内容），便于定位风控软拦截。
func searchDouyinFromJSON(ctx context.Context, actions plugin.SourceActions, accounts []douyinAccount, query string) ([]douyinUser, error) {
	var firstErr error
	for _, endpoint := range douyinSearchAPIURLs(query) {
		document, err := requestDouyinJSONAcrossAccounts(ctx, actions, accounts, endpoint, douyinSearchPageURL(query))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		users := collectDouyinUsers(document, query)
		if len(users) > 0 {
			return users, nil
		}
		logDouyinEmptySearchResult(ctx, actions, endpoint, document)
	}
	return nil, firstErr
}

// logDouyinEmptySearchResult 记录搜索空成功响应的结构摘要（状态码、顶层键、数据量），
// 不含响应内容，用于区分风控软拦截与解析问题。
func logDouyinEmptySearchResult(ctx context.Context, actions plugin.SourceActions, endpoint string, document map[string]any) {
	if actions == nil {
		return
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	endpointPath := douyinEndpointPath(endpoint)
	statusCode := plugin.IntScalar(document["status_code"])
	statusMessage := plugin.DiagnosticExcerpt(douyinDocumentMessage(document), 120)
	message := "未找到匹配的抖音用户，请尝试用户主页链接。"
	if statusMessage != "" {
		message += "平台提示：" + statusMessage
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   "info",
		Message: message,
		Fields: map[string]any{
			"endpoint":    endpointPath,
			"status_code": statusCode,
			"status_msg":  statusMessage,
			"top_keys":    strings.Join(keys, ","),
			"data_items":  len(plugin.SliceValue(document["data"])),
			"user_items":  len(plugin.SliceValue(document["user_list"])),
		},
	})
}

func matchDouyinUserByQuery(users []douyinUser, query string) *douyinUser {
	trimmed := strings.TrimSpace(query)
	if secUID := douyinSecUIDFromInput(trimmed); secUID != "" {
		for index := range users {
			if users[index].UID == secUID {
				return &users[index]
			}
		}
	}
	normalized := strings.TrimPrefix(trimmed, "@")
	for index := range users {
		if strings.EqualFold(strings.TrimSpace(users[index].Name), normalized) ||
			strings.EqualFold(strings.TrimSpace(users[index].UniqueID), normalized) {
			return &users[index]
		}
	}
	return nil
}

func douyinSecUIDFromInput(query string) string {
	text := strings.TrimSpace(query)
	if looksLikeDouyinSecUID(text) {
		return text
	}
	for _, raw := range plugin.URLPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		if !douyinAllowedRequestHost(parsed.Hostname()) {
			continue
		}
		for _, key := range []string{"sec_uid", "sec_user_id"} {
			if candidate := strings.TrimSpace(parsed.Query().Get(key)); looksLikeDouyinSecUID(candidate) {
				return candidate
			}
		}
		parts := plugin.PathParts(parsed.Path)
		for index, part := range parts {
			if part == "user" && index+1 < len(parts) && looksLikeDouyinSecUID(parts[index+1]) {
				return parts[index+1]
			}
		}
	}
	return ""
}

func douyinContentURLFromInput(query string) string {
	text := strings.TrimSpace(query)
	for _, raw := range plugin.URLPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if !douyinAllowedRequestHost(host) {
			continue
		}
		parts := plugin.PathParts(parsed.Path)
		if host == "v.douyin.com" || strings.HasSuffix(host, ".v.douyin.com") {
			return parsed.String()
		}
		for index, part := range parts {
			if (part == "video" || part == "note" || part == "share") && index+1 < len(parts) {
				return parsed.String()
			}
		}
	}
	return ""
}

func looksLikeDouyinSecUID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "MS4w") && len(value) >= 8
}

func douyinProfileAPIURL(secUID string) string {
	values := douyinWebParams()
	values.Set("sec_user_id", strings.TrimSpace(secUID))
	return douyinProfileOtherURL + "?" + values.Encode()
}

func douyinSearchAPIURLs(query string) []string {
	keyword := strings.TrimSpace(query)

	// 昵称搜索只请求当前用户频道。额外端点和搜索页回退会在一次
	// 用户操作中放大探测次数，因此页面回退由调用方最多执行一次。
	// 用户搜索走 discover/search（网页版同款端点）；search/item 对
	// type=user 已失效（返回视频混合结果）。版本参数对齐网页版 17.4.0。
	values := douyinWebParams()
	values.Set("version_code", "170400")
	values.Set("version_name", "17.4.0")
	values.Set("update_version_code", "170400")
	values.Set("keyword", keyword)
	values.Set("search_channel", "aweme_user_web")
	values.Set("search_source", "normal_search")
	values.Set("query_correct_type", "1")
	values.Set("is_filter_search", "0")
	values.Set("from_group_id", "")
	values.Set("disable_rs", "0")
	values.Set("offset", "0")
	values.Set("count", "12")
	values.Set("need_filter_settings", "1")
	values.Set("list_type", "single")
	values.Set("pc_search_top_1_params", `{"enable_ai_search_top_1":1}`)
	values.Set("support_h265", "1")
	values.Set("support_dash", "1")
	values.Set("cpu_core_num", "16")
	values.Set("device_memory", "8")
	values.Set("platform", "PC")
	values.Set("downlink", "10")
	values.Set("effective_type", "4g")
	values.Set("round_trip_time", "100")
	return []string{douyinDiscoverSearchURL + "?" + values.Encode()}
}

func douyinAwemePostAPIURL(secUID string) string {
	values := douyinWebParams()
	values.Set("sec_user_id", strings.TrimSpace(secUID))
	values.Set("count", "12")
	values.Set("max_cursor", "0")
	values.Set("locate_query", "false")
	values.Set("show_live_replay_strategy", "1")
	values.Set("need_time_list", "1")
	values.Set("time_list_query", "0")
	values.Set("whale_cut_token", "")
	values.Set("cut_version", "1")
	values.Set("publish_video_strategy_type", "2")
	values.Set("from_user_page", "1")
	values.Set("update_version_code", "170400")
	return douyinAwemePostURL + "?" + values.Encode()
}

func douyinUserPageURL(secUID string) string {
	return "https://www.douyin.com/user/" + url.PathEscape(strings.TrimSpace(secUID))
}

func douyinSearchPageURL(query string) string {
	return "https://www.douyin.com/search/" + url.PathEscape(strings.TrimSpace(query)) + "?type=user"
}

// douyinSubscriptionSourceFromInput resolves only identities already present
// in plugin state or explicitly carried by a profile URL/sec_uid. These inputs
// are sufficient to bind a subscription and do not need a nickname-search
// request.
func douyinSubscriptionSourceFromInput(current *plugin.Settings, query string) *douyinUser {
	explicitUID := douyinSecUIDFromInput(query)
	normalized := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	var matched *douyinUser
	if current != nil {
		for _, item := range current.Subscriptions {
			if item.Platform != "douyin" {
				continue
			}
			matches := explicitUID != "" && item.UID == explicitUID
			if explicitUID == "" {
				matches = strings.EqualFold(strings.TrimSpace(item.UID), normalized) || strings.EqualFold(strings.TrimSpace(item.Name), normalized)
			}
			if !matches {
				continue
			}
			if matched != nil && matched.UID != item.UID {
				return nil
			}
			candidate := douyinUser{UID: item.UID, Name: plugin.FirstText(item.Name, item.UID), AvatarURL: item.AvatarURL}
			if matched == nil || (matched.Name == matched.UID && candidate.Name != candidate.UID) {
				copy := candidate
				matched = &copy
			}
		}
	}
	if matched != nil {
		return matched
	}
	if explicitUID != "" {
		return &douyinUser{UID: explicitUID, Name: explicitUID}
	}
	return nil
}
