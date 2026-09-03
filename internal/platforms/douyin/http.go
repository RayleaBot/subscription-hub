package douyin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const douyinWebOrigin = "https://www.douyin.com"

const douyinWebReferer = "https://www.douyin.com/"

const douyinUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0.0.0 Safari/537.36"

const douyinShareUserAgent = "Mozilla/5.0 (Linux; Android 5.0; SM-G900P Build/LRX21T) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.25 Mobile Safari/537.36"

const douyinBrowserVersion = "151.0.0.0"

const douyinRequestTimeoutSeconds = 10

const douyinSearchTotalTimeout = 15 * time.Second

const douyinDetailTotalTimeout = 20 * time.Second

const douyinRedirectMaxHops = 5

const douyinWebCookiesTTL = 12 * time.Hour

const douyinProfileOtherURL = "https://www.douyin.com/aweme/v1/web/user/profile/other/"

const douyinDiscoverSearchURL = "https://www.douyin.com/aweme/v1/web/discover/search/"

const douyinAwemePostURL = "https://www.douyin.com/aweme/v1/web/aweme/post/"

const douyinAwemeDetailURL = "https://www.douyin.com/aweme/v1/web/aweme/detail/"

const douyinTTWIDRegisterURL = "https://ttwid.bytedance.com/ttwid/union/register/"

const douyinAnonTTWIDKey = "source:douyin:anon_ttwid"

const douyinWebIDURL = "https://mcs.zijieapi.com/webid?aid=6383&sdk_version=5.1.18_zip&device_platform=web"

const douyinMsTokenRegisterURL = "https://mssdk.bytedance.com/web/r/token?ms_appid=6383&msToken=" + douyinMsTokenSeed

const douyinMsTokenTTL = 2 * time.Hour

// douyinWebParams 对齐真实浏览器的公共查询参数；作品详情请求会基于
// 完整查询串追加 a_bogus，参数与 UA 必须保持一致。
func douyinWebParams() url.Values {
	return url.Values{
		"device_platform":  {"webapp"},
		"aid":              {"6383"},
		"channel":          {"channel_pc_web"},
		"pc_client_type":   {"1"},
		"pc_libra_divert":  {"Windows"},
		"version_code":     {"290100"},
		"version_name":     {"29.1.0"},
		"cookie_enabled":   {"true"},
		"screen_width":     {"1920"},
		"screen_height":    {"1080"},
		"browser_language": {"zh-CN"},
		"browser_platform": {"Win32"},
		"browser_name":     {"Chrome"},
		"browser_version":  {douyinBrowserVersion},
		"browser_online":   {"true"},
		"engine_name":      {"Blink"},
		"engine_version":   {douyinBrowserVersion},
		"os_name":          {"Windows"},
		"os_version":       {"10"},
		"cpu_core_num":     {"12"},
		"device_memory":    {"8"},
		"platform":         {"PC"},
		"downlink":         {"10"},
		"effective_type":   {"4g"},
		"round_trip_time":  {"100"},
		"support_h265":     {"1"},
		"support_dash":     {"0"},
	}
}

type douyinAccount struct {
	ID     string
	Label  string
	Cookie string
}

func (account douyinAccount) key() string {
	if value := strings.TrimSpace(account.ID); value != "" {
		return value
	}
	digest := sha256.Sum256([]byte(account.Cookie))
	return "cookie-" + hex.EncodeToString(digest[:6])
}

type douyinSourceError struct {
	Kind       string
	HTTPStatus int
	// StatusMsg 是抖音返回的公开错误文案（仅 JSON 文档的 status_msg 字段），用于诊断。
	StatusMsg  string
	Endpoint   string
	RetryAfter time.Duration
}

func (err *douyinSourceError) Error() string {
	if err == nil {
		return ""
	}
	kind := strings.TrimSpace(err.Kind)
	if kind == "" {
		kind = "upstream"
	}
	if err.HTTPStatus > 0 {
		return fmt.Sprintf("抖音上游请求失败（%s，HTTP %d）", kind, err.HTTPStatus)
	}
	return "抖音上游请求失败（" + kind + "）"
}

func (err *douyinSourceError) cooldown() bool {
	// auth（CK 失效）同样进冷却：失效会话继续探测会累积平台拦截，
	// 等待账号校验把凭据标记 invalid 后再恢复。
	return err != nil && (err.Kind == "auth" || err.Kind == "risk_control" || err.Kind == "rate_limit" || err.Kind == "session_blocked")
}

func readDouyinAccounts(ctx context.Context, actions plugin.SourceActions) ([]douyinAccount, error) {
	result, err := actions.ThirdPartyAccountRead(ctx, rayleabot.ThirdPartyAccountReadRequest{Platform: "douyin"})
	if err != nil {
		return nil, fmt.Errorf("抖音账号读取失败：%w", err)
	}
	accounts := make([]douyinAccount, 0)
	for _, raw := range plugin.SliceValue(result["accounts"]) {
		item := plugin.MapValue(raw)
		cookie := plugin.StringScalar(plugin.NestedValue(item, "cookie", "value"))
		if cookie == "" {
			continue
		}
		accounts = append(accounts, douyinAccount{
			ID:     plugin.StringScalar(item["account_id"]),
			Label:  plugin.StringScalar(item["label"]),
			Cookie: cookie,
		})
	}
	if len(accounts) == 0 {
		return nil, errors.New("没有可用的抖音账号 CK，请在 Web 三方账号页面保存账号")
	}
	return accounts, nil
}

func douyinRequestHeaders(cookie, referer, accept string) map[string]string {
	if strings.TrimSpace(referer) == "" {
		referer = douyinWebReferer
	}
	if strings.TrimSpace(accept) == "" {
		accept = "application/json, text/plain, */*"
	}
	headers := map[string]string{
		"Accept":             accept,
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"User-Agent":         douyinUserAgent,
		"Referer":            referer,
		"Origin":             douyinWebOrigin,
		"DNT":                "1",
		"Sec-GPC":            "1",
		"Sec-CH-UA":          `"Not=A?Brand";v="99", "Chromium";v="151", "Google Chrome";v="151"`,
		"Sec-CH-UA-Mobile":   "?0",
		"Sec-CH-UA-Platform": `"Windows"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-origin",
		"Cache-Control":      "no-cache",
	}
	if strings.TrimSpace(cookie) != "" {
		headers["Cookie"] = strings.TrimSpace(cookie)
	}
	return headers
}

func douyinShareRequestHeaders(referer string) map[string]string {
	if strings.TrimSpace(referer) == "" {
		referer = douyinWebReferer
	}
	return map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"User-Agent":      douyinShareUserAgent,
		"Referer":         referer,
	}
}

func douyinHasLoginCookie(cookie string) bool {
	for _, name := range []string{"sessionid", "sessionid_ss", "sid_guard"} {
		if plugin.CookieField(cookie, name) != "" {
			return true
		}
	}
	return false
}

type douyinSession struct {
	Cookie string
	FP     string
	WebID  string
}

type douyinClient struct {
	actions plugin.SourceActions
	now     func() time.Time
	// sessions 缓存本进程内各账号引导后的会话，需加锁读写。
	mu       sync.Mutex
	sessions map[string]douyinSession
}

func newDouyinClient(actions plugin.SourceActions) *douyinClient {
	return &douyinClient{actions: actions, now: time.Now, sessions: map[string]douyinSession{}}
}

func (client *douyinClient) requestJSON(ctx context.Context, rawURL string, account douyinAccount, referer string) (map[string]any, error) {
	session := client.session(ctx, account)
	signed := client.signAPIURL(rawURL, session)
	return client.requestJSONURL(ctx, rawURL, signed, account, session, referer)
}

func (client *douyinClient) requestDetailJSON(ctx context.Context, rawURL string, account douyinAccount, referer string) (map[string]any, error) {
	session := client.session(ctx, account)
	signed := client.signAPIURL(rawURL, session)
	signed = client.signDetailAPIURL(signed)
	return client.requestJSONURL(ctx, rawURL, signed, account, session, referer)
}

func (client *douyinClient) requestJSONURL(ctx context.Context, rawURL, signedURL string, account douyinAccount, session douyinSession, referer string) (map[string]any, error) {
	response, err := client.request(ctx, signedURL, douyinAccount{ID: account.ID, Label: account.Label, Cookie: session.Cookie}, referer, "application/json, text/plain, */*")
	if err != nil {
		return nil, err
	}
	endpoint := douyinEndpointPath(rawURL)
	status := int(plugin.IntScalar(response["status_code"]))
	document := plugin.DecodeHTTPDocument(response)
	if document == nil {
		kind := douyinErrorKind(status, douyinResponseBody(response))
		if status >= 200 && status < 300 {
			kind = "invalid_response"
		}
		sourceErr := &douyinSourceError{Kind: kind, HTTPStatus: status, Endpoint: endpoint}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return nil, sourceErr
	}
	kind := douyinClassifyError(status, document)
	if kind == "auth" || kind == "session_blocked" || kind == "risk_control" || kind == "rate_limit" || status < 200 || status >= 300 {
		sourceErr := &douyinSourceError{Kind: kind, HTTPStatus: status, StatusMsg: douyinDocumentMessage(document), Endpoint: endpoint}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return nil, sourceErr
	}
	if code := plugin.IntScalar(document["status_code"]); code != 0 {
		kind = douyinClassifyError(status, document)
		if kind == "upstream" {
			kind = "invalid_response"
		}
		sourceErr := &douyinSourceError{Kind: kind, HTTPStatus: status, StatusMsg: douyinDocumentMessage(document), Endpoint: endpoint}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return nil, sourceErr
	}
	if douyinSearchVerifyCheck(document) {
		sourceErr := &douyinSourceError{Kind: "risk_control", HTTPStatus: status, StatusMsg: "search_nil_info:verify_check", Endpoint: endpoint}
		return nil, sourceErr
	}
	return document, nil
}

func (client *douyinClient) requestHTML(ctx context.Context, rawURL string, account douyinAccount, referer string) (string, error) {
	session := client.session(ctx, account)
	response, err := client.request(ctx, rawURL, douyinAccount{ID: account.ID, Label: account.Label, Cookie: session.Cookie}, referer, "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if err != nil {
		return "", err
	}
	status := int(plugin.IntScalar(response["status_code"]))
	body := douyinResponseBody(response)
	kind := douyinHTMLBlockKind(status, body)
	if status < 200 || status >= 300 || kind == "auth" || kind == "session_blocked" || kind == "risk_control" || kind == "rate_limit" {
		sourceErr := &douyinSourceError{Kind: kind, HTTPStatus: status, Endpoint: douyinEndpointPath(rawURL)}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return "", sourceErr
	}
	return body, nil
}

func (client *douyinClient) requestShareHTML(ctx context.Context, rawURL string) (string, string, error) {
	headers := douyinShareRequestHeaders(douyinWebReferer)
	// 分享页 SSR 现在要求携带 ttwid 才返回作品数据；匿名 ttwid 即可，
	// 与登录 CK 无关（实测无 Cookie 时页面不含任何作品信息）。
	if ttwid := client.anonymousTTWID(ctx); ttwid != "" {
		headers["Cookie"] = "ttwid=" + ttwid
	}
	response, finalURL, err := client.requestFollowing(ctx, rawURL, headers)
	if err != nil {
		return "", finalURL, err
	}
	status := int(plugin.IntScalar(response["status_code"]))
	body := douyinResponseBody(response)
	kind := douyinHTMLBlockKind(status, body)
	if status < 200 || status >= 300 || kind == "auth" || kind == "session_blocked" || kind == "risk_control" || kind == "rate_limit" {
		return "", finalURL, &douyinSourceError{Kind: kind, HTTPStatus: status, Endpoint: douyinEndpointPath(finalURL)}
	}
	return body, finalURL, nil
}

func (client *douyinClient) request(ctx context.Context, rawURL string, account douyinAccount, referer, accept string) (rayleabot.ActionResult, error) {
	response, _, err := client.requestFollowing(ctx, rawURL, douyinRequestHeaders(account.Cookie, referer, accept))
	return response, err
}

func (client *douyinClient) requestFollowing(ctx context.Context, rawURL string, headers map[string]string) (rayleabot.ActionResult, string, error) {
	current := strings.TrimSpace(rawURL)
	var response rayleabot.ActionResult
	for hop := 0; hop <= douyinRedirectMaxHops; hop++ {
		result, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
			Method: "GET", URL: current, Headers: headers, TimeoutSeconds: douyinRequestTimeoutSeconds,
		})
		if err != nil {
			return nil, current, err
		}
		response = result
		status := int(plugin.IntScalar(result["status_code"]))
		if status < 300 || status >= 400 {
			return result, current, nil
		}
		location := douyinResponseHeader(result, "Location")
		if location == "" {
			return result, current, nil
		}
		next, err := url.Parse(location)
		if err != nil {
			return result, current, nil
		}
		if !next.IsAbs() {
			base, parseErr := url.Parse(current)
			if parseErr != nil {
				return result, current, nil
			}
			next = base.ResolveReference(next)
		}
		if !douyinAllowedRequestHost(next.Hostname()) {
			return result, current, nil
		}
		current = next.String()
	}
	return response, current, nil
}

// signAPIURL 使用账号会话中的 msToken、webid 和设备标识补齐 Web API 参数。
func (client *douyinClient) signAPIURL(rawURL string, session douyinSession) string {
	cookie := session.Cookie
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	if strings.TrimSpace(query.Get("msToken")) == "" {
		if token := plugin.CookieField(cookie, "msToken"); token != "" {
			query.Set("msToken", token)
		}
	}
	if strings.TrimSpace(query.Get("webid")) == "" && session.WebID != "" {
		query.Set("webid", session.WebID)
	}
	encoded := query.Encode()
	if verifyFP := plugin.CookieField(cookie, "s_v_web_id"); verifyFP != "" {
		encoded += "&verifyFp=" + url.QueryEscape(verifyFP) + "&fp=" + url.QueryEscape(verifyFP)
	}
	parsed.RawQuery = encoded
	return parsed.String()
}

func (client *douyinClient) signDetailAPIURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Query().Get("a_bogus") != "" {
		return rawURL
	}
	signature := douyinABogus(parsed.RawQuery, douyinUserAgent, client.now())
	if signature == "" {
		return rawURL
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery += "&"
	}
	parsed.RawQuery += "a_bogus=" + signature
	return parsed.String()
}

// session 返回账号的会话快照：引导补齐的 Cookie 与稳定的浏览器指纹。
// 指纹与 s_v_web_id 按账号持久化（KV），同一账号跨进程保持一致，避免指纹漂移触发风控。
func (client *douyinClient) session(ctx context.Context, account douyinAccount) douyinSession {
	cookie := strings.TrimSpace(account.Cookie)
	if cookie == "" {
		return douyinSession{Cookie: cookie, FP: douyinBrowserFingerprint()}
	}
	client.mu.Lock()
	cached, ok := client.sessions[account.key()]
	client.mu.Unlock()
	if ok {
		return cached
	}
	built := client.buildSession(ctx, account, cookie)
	client.mu.Lock()
	client.sessions[account.key()] = built
	client.mu.Unlock()
	return built
}

func (client *douyinClient) buildSession(ctx context.Context, account douyinAccount, cookie string) douyinSession {
	cookie = stripDouyinAnticheatCookie(cookie)
	key := "source:douyin:web_cookies:" + account.key()
	state := map[string]any{}
	if result, err := client.actions.KVGet(ctx, key); err == nil {
		if stored, exists := plugin.ActionStoredValue(result); exists {
			if object := plugin.MapValue(stored); object != nil {
				state = object
			}
		}
	}

	ttwid := plugin.FirstText(plugin.CookieField(cookie, "ttwid"), plugin.StringScalar(state["ttwid"]))
	// 引导得到的真实 msToken 优先于 CK 里的旧值；CK 值只作回退。
	msToken := plugin.FirstText(plugin.StringScalar(state["msToken"]), plugin.CookieField(cookie, "msToken"))
	svwID := plugin.FirstText(plugin.CookieField(cookie, "s_v_web_id"), plugin.StringScalar(state["s_v_web_id"]))
	webID := plugin.FirstText(plugin.CookieField(cookie, "webid"), plugin.StringScalar(state["webid"]))
	fp := plugin.StringScalar(state["fp"])
	changed := false

	// 引导条件：登录态 Cookie 缺少平台会话字段时补齐（ttwid/msToken/webid）。
	if ttwid == "" || msToken == "" || webID == "" {
		if client.bootstrapFresh(state) {
			// 命中新鲜缓存（含失败的负缓存），不重复请求。
		} else if fetched := client.bootstrapCookies(ctx, cookie); fetched != nil {
			if value := plugin.StringScalar(fetched["ttwid"]); value != "" && ttwid == "" {
				ttwid = value
			}
			if value := plugin.StringScalar(fetched["msToken"]); value != "" && msToken == "" {
				msToken = value
			}
			if value := plugin.StringScalar(fetched["s_v_web_id"]); value != "" && svwID == "" {
				svwID = value
			}
			if value := plugin.StringScalar(fetched["webid"]); value != "" && webID == "" {
				webID = value
			}
			state["ttwid"], state["msToken"], state["s_v_web_id"], state["webid"] = ttwid, msToken, svwID, webID
			state["fetched_at"] = client.now().Unix()
			changed = true
		} else {
			// 引导失败：写负缓存，30 分钟内不再重复请求。
			state["fetched_at"] = client.now().Unix()
			changed = true
		}
	}
	// msToken 独立于其他会话字段定期刷新：平台会校验 msToken 合法性，
	// 长时间复用 CK 里的旧值会触发风控；刷新失败回退 CK 值或随机令牌。
	if msTokenRefreshDue(state, client.now()) {
		if fresh := client.bootstrapMsToken(ctx); fresh != "" {
			state["msToken"] = fresh
			msToken = fresh
		}
		state["ms_token_fetched_at"] = client.now().Unix()
		changed = true
	}
	// s_v_web_id 由浏览器 JS 生成，服务器从不下发；缺失时本地生成并持久化。
	if svwID == "" {
		svwID = douyinVerifyFp()
		changed = true
	}
	if fp == "" {
		fp = douyinBrowserFingerprint()
		changed = true
	}
	if changed {
		state["ttwid"], state["msToken"], state["s_v_web_id"], state["webid"], state["fp"] = ttwid, msToken, svwID, webID, fp
		if _, exists := state["fetched_at"]; !exists {
			state["fetched_at"] = client.now().Unix()
		}
		_, _ = client.actions.KVSet(ctx, key, state)
	}
	merged := mergeDouyinSessionCookies(cookie, ttwid, msToken, svwID)
	// 引导得到的真实 msToken 覆盖 CK 里的旧值（merge 只在缺失时追加，不覆盖）。
	merged = douyinCookieSet(merged, "msToken", msToken)
	return douyinSession{Cookie: merged, FP: fp, WebID: webID}
}

// msTokenRefreshDue 判断引导 msToken 是否需要刷新：成功引导按 2 小时 TTL，
// 失败按 30 分钟负缓存，避免频繁请求 mssdk 端点。
func msTokenRefreshDue(state map[string]any, now time.Time) bool {
	fetchedAt := plugin.IntScalar(state["ms_token_fetched_at"])
	if fetchedAt <= 0 {
		return true
	}
	ttl := douyinMsTokenTTL
	if plugin.StringScalar(state["msToken"]) == "" {
		ttl = 30 * time.Minute
	}
	return now.Unix()-fetchedAt >= int64(ttl/time.Second)
}

// douyinCookieSet 设置 Cookie 中的单个字段（存在则覆盖，缺失则追加），
// 用于让引导得到的会话值覆盖 CK 里可能已过期的旧值。
func douyinCookieSet(cookie, name, value string) string {
	if value == "" {
		return cookie
	}
	parts := strings.Split(cookie, ";")
	found := false
	for index, part := range parts {
		key, _, has := strings.Cut(strings.TrimSpace(part), "=")
		if has && strings.TrimSpace(key) == name {
			parts[index] = " " + name + "=" + value
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, " "+name+"="+value)
	}
	return strings.Join(parts, ";")
}

// bootstrapFresh 判断缓存的引导结果是否仍在有效期；失败结果只缓存 30 分钟。
func (client *douyinClient) bootstrapFresh(state map[string]any) bool {
	fetchedAt := plugin.IntScalar(state["fetched_at"])
	if fetchedAt <= 0 {
		return false
	}
	ttl := douyinWebCookiesTTL
	if plugin.StringScalar(state["ttwid"]) == "" && plugin.StringScalar(state["msToken"]) == "" && plugin.StringScalar(state["webid"]) == "" {
		ttl = 30 * time.Minute
	}
	return client.now().Unix()-fetchedAt < int64(ttl/time.Second)
}

// bootstrapCookies 依次补齐缺失的会话字段：先访问首页（登录态 CK 会一次拿到
// ttwid/msToken），再按需走 ttwid 联合注册与 webid 分配端点（匿名可用）。
// 返回 nil 表示完全没有引导到任何字段。
func (client *douyinClient) bootstrapCookies(ctx context.Context, cookie string) map[string]any {
	values := map[string]any{}
	if plugin.CookieField(cookie, "ttwid") == "" || plugin.CookieField(cookie, "msToken") == "" {
		if response, err := client.request(ctx, douyinWebOrigin+"/", douyinAccount{Cookie: cookie}, "", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"); err == nil {
			setCookie := douyinResponseHeader(response, "Set-Cookie")
			if ttwid := douyinSetCookieValue(setCookie, "ttwid"); ttwid != "" {
				values["ttwid"] = ttwid
			}
			if msToken := douyinSetCookieValue(setCookie, "msToken"); msToken != "" {
				values["msToken"] = msToken
			}
			if svwID := douyinSetCookieValue(setCookie, "s_v_web_id"); svwID != "" {
				values["s_v_web_id"] = svwID
			}
		}
	}
	if values["ttwid"] == nil {
		if ttwid := client.bootstrapTTWID(ctx, cookie); ttwid != "" {
			values["ttwid"] = ttwid
		}
	}
	if webID := client.bootstrapWebID(ctx); webID != "" {
		values["webid"] = webID
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// bootstrapTTWID 通过 ttwid 联合注册端点获取 ttwid（真实浏览器的无 Cookie 入口）。
func (client *douyinClient) bootstrapTTWID(ctx context.Context, cookie string) string {
	result, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "POST", URL: douyinTTWIDRegisterURL,
		Headers: map[string]string{
			"Content-Type": "application/json", "User-Agent": douyinUserAgent, "Referer": douyinWebReferer, "Cookie": cookie,
		},
		BodyText:       `{"region":"cn","aid":6383,"needFid":false,"service":"www.douyin.com","migrate_info":{"ticket":"","source":"node"},"cbUrlProtocol":"https","union":true}`,
		TimeoutSeconds: douyinRequestTimeoutSeconds,
	})
	if err != nil || plugin.IntScalar(result["status_code"]) != 200 {
		return ""
	}
	return douyinSetCookieValue(douyinResponseHeader(result, "Set-Cookie"), "ttwid")
}

// anonymousTTWID 返回共享的匿名 ttwid：优先读 KV 缓存的注册结果，缺失时
// 通过 ttwid 联合注册端点获取并持久化（注册的 ttwid 有效期约一年）。
// 分享页 SSR 与作品详情端点只有在携带 ttwid 时才返回完整数据，登录 CK 与
// a_bogus 签名都不是必要条件（实测无签名 + 匿名 ttwid 即可通过）。
// 注册失败返回空串，调用方按无 Cookie 请求回退。
func (client *douyinClient) anonymousTTWID(ctx context.Context) string {
	key := douyinAnonTTWIDKey
	if result, err := client.actions.KVGet(ctx, key); err == nil {
		if stored, exists := plugin.ActionStoredValue(result); exists {
			if object := plugin.MapValue(stored); object != nil {
				if ttwid := strings.TrimSpace(plugin.StringScalar(object["ttwid"])); ttwid != "" {
					return ttwid
				}
			}
		}
	}
	registered := client.bootstrapTTWID(ctx, "")
	if registered == "" {
		return ""
	}
	_, _ = client.actions.KVSet(ctx, key, map[string]any{"ttwid": registered})
	return registered
}

// requestDetailJSONAnonymous 用匿名会话（仅匿名 ttwid，无登录 CK、无 a_bogus）
// 请求作品详情端点。登录 CK 缺失或全部失效时，这条链仍然可以解析抖音链接，
// 与 R-plugin 的无签名 HTTP 链一致。
func (client *douyinClient) requestDetailJSONAnonymous(ctx context.Context, rawURL, referer string) (map[string]any, error) {
	cookie := ""
	if ttwid := client.anonymousTTWID(ctx); ttwid != "" {
		cookie = "ttwid=" + ttwid
	}
	account := douyinAccount{Cookie: cookie}
	return client.requestJSONURL(ctx, rawURL, rawURL, account, douyinSession{Cookie: cookie}, referer)
}

// bootstrapMsToken 从 mssdk 端点获取真实 msToken（与 f2 TokenManager.gen_real_msToken 同源）。
// 平台服务端会校验 msToken 的合法性：CK 里长期保存的旧值或本地随机值都可能触发风控。
// 失败返回空串，调用方回退 CK 值或随机令牌。
func (client *douyinClient) bootstrapMsToken(ctx context.Context) string {
	payload := `{"magic":538969122,"version":1,"dataType":8,"strData":"` + douyinMsTokenData + `","ulr":0,"tspFromClient":` + strconv.FormatInt(client.now().UnixMilli(), 10) + `}`
	result, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "POST", URL: douyinMsTokenRegisterURL,
		Headers:        map[string]string{"Content-Type": "application/json; charset=utf-8", "User-Agent": douyinUserAgent},
		BodyText:       payload,
		TimeoutSeconds: douyinRequestTimeoutSeconds,
	})
	if err != nil || plugin.IntScalar(result["status_code"]) != 200 {
		return ""
	}
	msToken := douyinSetCookieValue(douyinResponseHeader(result, "Set-Cookie"), "msToken")
	if len(msToken) != 164 && len(msToken) != 184 {
		return ""
	}
	return msToken
}

// bootstrapWebID 分配数字 webid（真实浏览器每个 API 请求都携带的追踪标识）。
func (client *douyinClient) bootstrapWebID(ctx context.Context) string {
	result, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "POST", URL: douyinWebIDURL,
		Headers:        map[string]string{"Content-Type": "application/json; charset=UTF-8", "User-Agent": douyinUserAgent, "Referer": douyinWebReferer},
		BodyText:       `{"app_id":6383,"referer":"https://www.douyin.com/","url":"https://www.douyin.com/","user_agent":"` + douyinUserAgent + `","user_unique_id":""}`,
		TimeoutSeconds: douyinRequestTimeoutSeconds,
	})
	if err != nil || plugin.IntScalar(result["status_code"]) != 200 {
		return ""
	}
	document := plugin.DecodeHTTPDocument(result)
	return plugin.FirstText(plugin.StringScalar(document["web_id"]), plugin.StringScalar(document["webId"]), plugin.StringScalar(document["webid"]))
}

func mergeDouyinSessionCookies(cookie, ttwid, msToken, svwID string) string {
	for _, pair := range [][2]string{{"ttwid", ttwid}, {"msToken", msToken}, {"s_v_web_id", svwID}} {
		if pair[1] != "" && plugin.CookieField(cookie, pair[0]) == "" {
			cookie = strings.TrimSuffix(cookie, ";") + "; " + pair[0] + "=" + pair[1]
		}
	}
	return cookie
}

// stripDouyinAnticheatCookie 从 Cookie 中移除 __ac_nonce 反作弊标记：
// 服务端在每次页面响应中都会重种该值，程序持有的旧值会触发验证中间页，
// 且无法维持新鲜值；实测剥离后页面恢复正常、API 行为不变。
func stripDouyinAnticheatCookie(cookie string) string {
	fields := strings.Split(cookie, ";")
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		name, _, _ := strings.Cut(strings.TrimSpace(field), "=")
		if strings.EqualFold(strings.TrimSpace(name), "__ac_nonce") || strings.TrimSpace(name) == "" {
			continue
		}
		kept = append(kept, strings.TrimSpace(field))
	}
	return strings.Join(kept, "; ")
}

// douyinSearchVerifyCheck 识别搜索响应的 verify_check 空壳：status_code=0、
// 结果为空，但 search_nil_info.search_nil_type=verify_check 表明服务端明确
// 要求当前会话先通过人工验证，与签名、msToken、请求形态无关，
// 继续探测只会加剧风控。
func douyinSearchVerifyCheck(document map[string]any) bool {
	searchNil := plugin.MapValue(document["search_nil_info"])
	return strings.TrimSpace(plugin.StringScalar(searchNil["search_nil_type"])) == "verify_check"
}

// douyinEndpointPath 返回请求的主机+路径（不含查询串），用于诊断日志定位接口。
func douyinEndpointPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Host) + strings.TrimRight(parsed.Path, "/")
}

// douyinSetCookieValue 从（可能被合并成单行的）Set-Cookie 头里提取指定 Cookie 值。
func douyinSetCookieValue(header, name string) string {
	pattern := regexp.MustCompile(`(?:^|,\s*)` + regexp.QuoteMeta(name) + `=([^;]+)`)
	match := pattern.FindStringSubmatch(header)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func (client *douyinClient) requestCredentialValidation(ctx context.Context, account douyinAccount, sourceErr *douyinSourceError) {
	if client == nil || client.actions == nil || sourceErr == nil || strings.TrimSpace(account.ID) == "" {
		return
	}
	caller, ok := client.actions.(plugin.GenericLocalActionCaller)
	if !ok {
		return
	}
	observation := ""
	switch sourceErr.Kind {
	case "auth":
		observation = "auth_rejected"
	case "session_blocked":
		observation = "session_blocked"
	default:
		return
	}
	var result rayleabot.ActionResult
	_ = caller.Call(ctx, "thirdparty.account.validate", plugin.AccountValidationRequest{
		Platform:    "douyin",
		AccountID:   strings.TrimSpace(account.ID),
		Observation: observation,
		HTTPStatus:  sourceErr.HTTPStatus,
	}, &result)
}

func requestDouyinJSONAcrossAccounts(ctx context.Context, actions plugin.SourceActions, accounts []douyinAccount, rawURL, referer string) (map[string]any, error) {
	client := newDouyinClient(actions)
	var lastError error
	for _, account := range accounts {
		document, err := client.requestJSON(ctx, rawURL, account, referer)
		if err == nil {
			return document, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("没有可用的抖音账号")
	}
	return nil, lastError
}

func requestDouyinHTMLAcrossAccounts(ctx context.Context, actions plugin.SourceActions, accounts []douyinAccount, rawURL, referer string) (string, error) {
	client := newDouyinClient(actions)
	var lastError error
	for _, account := range accounts {
		body, err := client.requestHTML(ctx, rawURL, account, referer)
		if err == nil {
			return body, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("没有可用的抖音账号")
	}
	return "", lastError
}

func douyinErrorKind(status int, body string) string {
	switch {
	case status == 401:
		return "auth"
	case status == 403 || status == 432:
		return "session_blocked"
	case status == 412 || status == 418:
		return "risk_control"
	case status == 429:
		return "rate_limit"
	case status >= 500:
		return "server"
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{"验证码", "captcha", "风控", "频繁", "拦截"} {
		if strings.Contains(lower, marker) {
			return "risk_control"
		}
	}
	for _, marker := range []string{"登录", "登陆", "login", "未登录", "expired"} {
		if strings.Contains(lower, marker) {
			return "auth"
		}
	}
	return "upstream"
}

// douyinHTMLBlockKind only treats explicit interstitial signals as a blocked
// page. Normal Douyin HTML contains login and security-related strings in its
// navigation and scripts, so scanning the full document for broad keywords
// produces false authentication and risk-control results.
func douyinHTMLBlockKind(status int, body string) string {
	if kind := douyinErrorKind(status, ""); kind != "upstream" {
		return kind
	}
	if len(douyinDocumentsFromHTML(body)) > 0 {
		return "upstream"
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"验证码中间页",
		"为了你的账号安全，请先完成验证",
		"verifycenter_nocaptcha",
	} {
		if strings.Contains(lower, marker) {
			return "risk_control"
		}
	}
	return "upstream"
}

func douyinClassifyError(status int, document map[string]any) string {
	kind := douyinErrorKind(status, douyinDocumentMessage(document))
	if kind == "auth" || kind == "risk_control" || kind == "rate_limit" || kind == "session_blocked" {
		return kind
	}
	if douyinDocumentLooksLikeAuth(document) {
		return "auth"
	}
	if douyinDocumentLooksLikeRisk(document) {
		return "risk_control"
	}
	return kind
}

func douyinDocumentMessage(document map[string]any) string {
	return strings.ToLower(plugin.FirstText(
		document["status_msg"], document["message"], document["msg"],
		plugin.NestedValue(document, "data", "status_msg"), plugin.NestedValue(document, "data", "message"),
	))
}

func douyinDocumentLooksLikeAuth(document map[string]any) bool {
	code := plugin.IntScalar(document["status_code"])
	if code == 8 || code == 307 || code == 2154 || code == 2483 {
		return true
	}
	message := douyinDocumentMessage(document)
	for _, marker := range []string{"登录", "登陆", "login", "未登录", "expired"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func douyinDocumentLooksLikeRisk(document map[string]any) bool {
	message := douyinDocumentMessage(document)
	for _, marker := range []string{"风控", "验证", "captcha", "安全", "频繁", "拦截"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func douyinResponseBody(result rayleabot.ActionResult) string {
	if text := plugin.StringScalar(result["body_text"]); text != "" {
		return text
	}
	encoded := plugin.StringScalar(result["body_base64"])
	if encoded == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !utf8.Valid(raw) {
		return ""
	}
	return string(raw)
}

func douyinResponseHeader(result rayleabot.ActionResult, name string) string {
	headers := plugin.MapValue(result["headers"])
	for key, value := range headers {
		if !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		switch typed := value.(type) {
		case []any:
			if len(typed) > 0 {
				return strings.TrimSpace(plugin.StringScalar(typed[0]))
			}
		case []string:
			if len(typed) > 0 {
				return strings.TrimSpace(typed[0])
			}
		default:
			return strings.TrimSpace(plugin.StringScalar(value))
		}
	}
	return ""
}

func douyinAllowedRequestHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, suffix := range []string{"douyin.com", "iesdouyin.com", "amemv.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func douyinErrorLogFields(err error) map[string]any {
	fields := map[string]any{}
	var sourceErr *douyinSourceError
	if errors.As(err, &sourceErr) {
		fields["kind"] = plugin.FirstText(sourceErr.Kind, "upstream")
		if sourceErr.HTTPStatus > 0 {
			fields["http_status"] = sourceErr.HTTPStatus
		}
		if sourceErr.Endpoint != "" {
			fields["endpoint"] = sourceErr.Endpoint
		}
		if sourceErr.StatusMsg != "" {
			fields["status_msg"] = plugin.DiagnosticExcerpt(sourceErr.StatusMsg, 120)
		}
		if sourceErr.RetryAfter > 0 {
			fields["retry_after_seconds"] = int(sourceErr.RetryAfter / time.Second)
		}
		return fields
	}
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		fields["kind"] = "local_action"
		if strings.TrimSpace(actionErr.Code) != "" {
			fields["error_code"] = strings.TrimSpace(actionErr.Code)
		}
		return fields
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fields["kind"] = "timeout"
	} else if errors.Is(err, context.Canceled) {
		fields["kind"] = "canceled"
	} else if err != nil {
		fields["kind"] = "request"
	}
	return fields
}

func friendlyDouyinSourceError(label string, err error) string {
	var sourceErr *douyinSourceError
	if !errors.As(err, &sourceErr) {
		if plugin.IsHTTPActionPermissionError(err) {
			return label + "：请检查插件 http.request 权限与宿主网络安全策略。"
		}
		if err != nil && strings.Contains(err.Error(), "没有可用的抖音账号") {
			return label + "：" + strings.TrimSpace(err.Error()) + "。"
		}
		return label + "。"
	}
	switch sourceErr.Kind {
	case "search_paused":
		minutes := int((sourceErr.RetryAfter + time.Minute - 1) / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("%s：抖音昵称搜索已暂停，剩余约 %d 分钟。请直接使用用户主页链接或完整 sec_uid 订阅。", label, minutes)
	case "search_unavailable":
		return label + "：抖音没有返回可用的用户结果。为避免继续触发安全验证，请直接使用用户主页链接或完整 sec_uid 订阅；此结果不能说明 CK 已失效。"
	case "session_blocked":
		status := sourceErr.HTTPStatus
		if status <= 0 {
			return label + "：抖音接口拒绝当前请求，不能据此判定 CK 失效；请以三方账号页的服务器检查结果为准。"
		}
		return fmt.Sprintf("%s：抖音接口拒绝当前请求（HTTP %d），不能据此判定 CK 失效；请以三方账号页的服务器检查结果为准。", label, status)
	case "risk_control":
		if strings.Contains(label, "搜索") {
			return label + "：抖音要求安全验证，本次已停止继续探测。请在浏览器中打开抖音完成一次搜索（按提示通过验证）后，重新获取 Cookie 更新到账号页；也可以直接使用用户主页链接或完整 sec_uid 订阅（该路径不受影响）；此结果不能说明 CK 已失效。"
		}
		return label + "：抖音请求被安全验证拦截，请稍后再试；此结果不能单独说明 CK 已失效。"
	case "rate_limit":
		return label + "：抖音请求过于频繁，请稍后再试。"
	case "auth":
		return label + "：抖音账号 CK 已失效，请重新扫码。"
	default:
		if sourceErr.HTTPStatus > 0 {
			return fmt.Sprintf("%s：抖音上游请求异常（%s，HTTP %d）。", label, plugin.FirstText(sourceErr.Kind, "upstream"), sourceErr.HTTPStatus)
		}
		return fmt.Sprintf("%s：抖音上游请求异常（%s）。", label, plugin.FirstText(sourceErr.Kind, "upstream"))
	}
}

func friendlyDouyinError(err error) string {
	if err == nil {
		return "没有找到匹配的抖音用户。"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "抖音用户信息读取失败。"
	}
	if !strings.HasSuffix(message, "。") {
		message += "。"
	}
	return message
}
