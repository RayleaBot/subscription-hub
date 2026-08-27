package plugin

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	douyinSearchResultLimit       = 10
	douyinSearchPauseKey          = "source:douyin:search:pause"
	douyinSearchEmptyPause        = 5 * time.Minute
	douyinSearchVerificationPause = 30 * time.Minute
	douyinHostResolveTimeout      = 70 * time.Second
)

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

func resolveDouyinUsers(ctx context.Context, event *rayleabot.EventContext, query string) ([]douyinUser, error) {
	return resolveDouyinUsersWithActions(ctx, event.Actions(), query)
}

func resolveDouyinUsersWithActions(ctx context.Context, actions pluginActions, query string) ([]douyinUser, error) {
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

func readDouyinUserWithActions(ctx context.Context, actions pluginActions, secUID string) (douyinUser, error) {
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

func readDouyinUserFromJSON(ctx context.Context, actions pluginActions, accounts []douyinAccount, secUID string) (douyinUser, error) {
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

func readDouyinAuthorFromURL(ctx context.Context, actions pluginActions, rawURL string) (douyinUser, error) {
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

func searchDouyin(ctx context.Context, event *rayleabot.EventContext, query string) ([]douyinUser, error) {
	return searchDouyinWithActions(ctx, event.Actions(), query)
}

func searchDouyinWithActions(ctx context.Context, actions pluginActions, query string) ([]douyinUser, error) {
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
				if resolved, resolveErr := searchDouyinViaHostResolve(ctx, actions, query, resolveCookie); resolveErr == nil && len(resolved) > 0 {
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
		if resolved, resolveErr := searchDouyinViaHostResolve(ctx, actions, query, resolveCookie); resolveErr == nil && len(resolved) > 0 {
			clearDouyinSearchPause(ctx, actions)
			return resolved, nil
		} else if resolveErr != nil {
			logDouyinFailure(ctx, actions, "抖音宿主浏览器解析失败", resolveErr)
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

// thirdPartyResolveRequest 请求宿主用该平台的登录环境（浏览器会话与设备
// 信誉）解析昵称关键词。纯 HTTP 搜索被风控拦截时这是唯一可用的路径。
// Cookie 为账号 CK（敏感凭据），宿主只用于恢复登录态，不落日志。
type thirdPartyResolveRequest struct {
	Platform string `json:"platform"`
	Query    string `json:"query"`
	Cookie   string `json:"cookie,omitempty"`
}

// searchDouyinViaHostResolve 在纯 HTTP 搜索无结果后请求宿主登录 profile
// 浏览器解析关键词。宿主浏览器与扫码登录窗口互斥；失败只记录诊断日志，
// 不改变既有 HTTP 失败的结论。
func searchDouyinViaHostResolve(ctx context.Context, actions pluginActions, query, cookie string) ([]douyinUser, error) {
	caller, ok := actions.(genericLocalActionCaller)
	if !ok {
		return nil, errors.New("宿主不支持 local action 调用")
	}
	resolveCtx, cancel := context.WithTimeout(ctx, douyinHostResolveTimeout)
	defer cancel()
	var result rayleabot.ActionResult
	if err := caller.Call(resolveCtx, "thirdparty.resolve", thirdPartyResolveRequest{
		Platform: "douyin",
		Query:    strings.TrimSpace(query),
		Cookie:   cookie,
	}, &result); err != nil {
		return nil, err
	}
	return douyinUsersFromResolveResult(result), nil
}

func douyinUsersFromResolveResult(result rayleabot.ActionResult) []douyinUser {
	users := make([]douyinUser, 0)
	for _, raw := range sliceValue(result["profiles"]) {
		item := mapValue(raw)
		if item == nil {
			continue
		}
		uid := stringScalar(item["uid"])
		name := stringScalar(item["nickname"])
		if uid == "" || name == "" {
			continue
		}
		users = append(users, douyinUser{
			UID:       uid,
			UniqueID:  stringScalar(item["unique_id"]),
			Name:      name,
			AvatarURL: stringScalar(item["avatar_url"]),
		})
	}
	return users
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

func douyinSearchPauseRemaining(ctx context.Context, actions pluginActions, now time.Time) (time.Duration, string) {
	if actions == nil {
		return 0, ""
	}
	result, err := actions.KVGet(ctx, douyinSearchPauseKey)
	if err != nil {
		return 0, ""
	}
	stored, exists := actionStoredValue(result)
	if !exists {
		return 0, ""
	}
	state := mapValue(stored)
	remaining := time.Unix(intScalar(state["until"]), 0).Sub(now)
	if remaining <= 0 {
		return 0, ""
	}
	return remaining, stringScalar(state["kind"])
}

func rememberDouyinSearchPause(ctx context.Context, actions pluginActions, err error, delay time.Duration, now time.Time) {
	if actions == nil || delay <= 0 {
		return
	}
	kind := "upstream"
	var sourceErr *douyinSourceError
	if errors.As(err, &sourceErr) {
		kind = firstText(sourceErr.Kind, kind)
	}
	_, _ = actions.KVSet(ctx, douyinSearchPauseKey, map[string]any{
		"kind": kind, "until": now.Add(delay).Unix(),
	})
}

func clearDouyinSearchPause(ctx context.Context, actions pluginActions) {
	if actions == nil {
		return
	}
	if result, err := actions.KVGet(ctx, douyinSearchPauseKey); err == nil {
		if _, exists := actionStoredValue(result); exists {
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
func searchDouyinFromJSON(ctx context.Context, actions pluginActions, accounts []douyinAccount, query string) ([]douyinUser, error) {
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
func logDouyinEmptySearchResult(ctx context.Context, actions pluginActions, endpoint string, document map[string]any) {
	if actions == nil {
		return
	}
	keys := make([]string, 0, len(document))
	for key := range document {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   "info",
		Message: "抖音搜索返回空结果",
		Fields: map[string]any{
			"endpoint":    douyinEndpointPath(endpoint),
			"status_code": intScalar(document["status_code"]),
			"status_msg":  diagnosticExcerpt(douyinDocumentMessage(document), 120),
			"top_keys":    strings.Join(keys, ","),
			"data_items":  len(sliceValue(document["data"])),
			"user_items":  len(sliceValue(document["user_list"])),
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
	for _, raw := range urlPattern.FindAllString(text, -1) {
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
		parts := pathParts(parsed.Path)
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
	for _, raw := range urlPattern.FindAllString(text, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if !douyinAllowedRequestHost(host) {
			continue
		}
		parts := pathParts(parsed.Path)
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
