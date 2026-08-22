package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	weiboMobileContainerURL = "https://m.weibo.cn/api/container/getIndex"
	weiboStatusShowURL      = "https://m.weibo.cn/statuses/show"
	weiboLongTextURL        = "https://m.weibo.cn/statuses/extend"
	weiboWebUserSearchURL   = "https://s.weibo.com/user"
	weiboMobileReferer      = "https://m.weibo.cn/"
	weiboWebSearchReferer   = "https://s.weibo.com/"

	// 微博请求单发 10s；搜索/详情整体各 15s，确保回复在插件事件 60s 超时前发出
	//（还需为头像内联与渲染留出时间）。
	weiboRequestTimeoutSeconds = 10
	weiboSearchTotalTimeout    = 15 * time.Second
	weiboDetailTotalTimeout    = 15 * time.Second
)

var weiboSecretPattern = regexp.MustCompile(`(?i)(["']?)(SUBP?|XSRF-TOKEN|X-CSRF-TOKEN|_T_WM|MLOGIN|user_token)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^;,\s]+)`)

type weiboAccount struct {
	ID     string
	Label  string
	Cookie string
}

func (account weiboAccount) key() string {
	if value := strings.TrimSpace(account.ID); value != "" {
		return value
	}
	digest := sha256.Sum256([]byte(account.Cookie))
	return "cookie-" + hex.EncodeToString(digest[:6])
}

type weiboSourceError struct {
	Kind       string
	HTTPStatus int
}

type thirdPartyAccountValidateRequest struct {
	Platform    string `json:"platform"`
	AccountID   string `json:"account_id"`
	Observation string `json:"observation"`
	HTTPStatus  int    `json:"http_status,omitempty"`
}

func (err *weiboSourceError) Error() string {
	if err == nil {
		return ""
	}
	kind := strings.TrimSpace(err.Kind)
	if kind == "" {
		kind = "upstream"
	}
	if err.HTTPStatus > 0 {
		return fmt.Sprintf("微博上游请求失败（%s，HTTP %d）", kind, err.HTTPStatus)
	}
	return "微博上游请求失败（" + kind + "）"
}

func (err *weiboSourceError) cooldown() bool {
	return err != nil && (err.Kind == "risk_control" || err.Kind == "rate_limit" || err.Kind == "session_blocked")
}

func readWeiboAccounts(ctx context.Context, actions pluginActions) ([]weiboAccount, error) {
	result, err := actions.ThirdPartyAccountRead(ctx, rayleabot.ThirdPartyAccountReadRequest{Platform: "weibo"})
	if err != nil {
		return nil, fmt.Errorf("微博账号读取失败：%w", err)
	}
	accounts := make([]weiboAccount, 0)
	for _, raw := range sliceValue(result["accounts"]) {
		item := mapValue(raw)
		cookie := stringScalar(nestedValue(item, "cookie", "value"))
		if cookie == "" {
			continue
		}
		accounts = append(accounts, weiboAccount{
			ID:     stringScalar(item["account_id"]),
			Label:  stringScalar(item["label"]),
			Cookie: cookie,
		})
	}
	if len(accounts) == 0 {
		return nil, errors.New("没有可用的微博账号 CK，请在 Web 三方账号页面保存账号")
	}
	return accounts, nil
}

func weiboRequestHeaders(cookie, referer string) map[string]string {
	if strings.TrimSpace(referer) == "" {
		referer = weiboMobileReferer
	}
	headers := map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"User-Agent":         bilibiliUserAgent,
		"Referer":            referer,
		"DNT":                "1",
		"Sec-GPC":            "1",
		"Sec-CH-UA":          `"Chromium";v="134", "Google Chrome";v="134", "Not?A_Brand";v="99"`,
		"Sec-CH-UA-Mobile":   "?0",
		"Sec-CH-UA-Platform": `"Windows"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-origin",
		"Cache-Control":      "no-cache",
		"X-Requested-With":   "XMLHttpRequest",
	}
	if strings.TrimSpace(cookie) != "" {
		headers["Cookie"] = strings.TrimSpace(cookie)
		if csrf := cookieField(cookie, "X-CSRF-TOKEN"); csrf != "" {
			headers["x-csrf-token"] = csrf
		}
	}
	return headers
}

// weiboSearchPageHeaders 返回 s.weibo.com 网页搜索的请求头（HTML 页面，非 JSON 接口）。
func weiboSearchPageHeaders(cookie string) map[string]string {
	headers := map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"User-Agent":      bilibiliUserAgent,
		"Referer":         weiboWebSearchReferer,
		"Cache-Control":   "no-cache",
	}
	if strings.TrimSpace(cookie) != "" {
		headers["Cookie"] = strings.TrimSpace(cookie)
	}
	return headers
}

func weiboUserContainerURL(uid string) string {
	uid = strings.TrimSpace(uid)
	values := url.Values{}
	values.Set("type", "uid")
	values.Set("value", uid)
	values.Set("containerid", "100505"+uid)
	return weiboMobileContainerURL + "?" + values.Encode()
}

func weiboUserFeedURL(uid string, cursor ...string) string {
	uid = strings.TrimSpace(uid)
	values := url.Values{}
	values.Set("type", "uid")
	values.Set("value", uid)
	values.Set("containerid", "107603"+uid)
	if len(cursor) > 0 {
		if sinceID := strings.TrimSpace(cursor[0]); sinceID != "" && sinceID != "0" {
			values.Set("since_id", sinceID)
		}
	}
	return weiboMobileContainerURL + "?" + values.Encode()
}

func weiboStatusShowEndpoint(id string) string {
	values := url.Values{}
	values.Set("id", strings.TrimSpace(id))
	return weiboStatusShowURL + "?" + values.Encode()
}

func weiboLongTextEndpoint(id string) string {
	values := url.Values{}
	values.Set("id", strings.TrimSpace(id))
	return weiboLongTextURL + "?" + values.Encode()
}

type weiboClient struct {
	actions pluginActions
	now     func() time.Time
}

func newWeiboClient(actions pluginActions) *weiboClient {
	return &weiboClient{actions: actions, now: time.Now}
}

func (client *weiboClient) requestJSON(ctx context.Context, rawURL string, account weiboAccount, referer string) (map[string]any, error) {
	response, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "GET", URL: rawURL, Headers: weiboRequestHeaders(account.Cookie, referer), TimeoutSeconds: weiboRequestTimeoutSeconds,
	})
	if err != nil {
		return nil, err
	}
	status := int(intScalar(response["status_code"]))
	document := decodeBilibiliDocument(response)
	if document == nil {
		kind := weiboErrorKind(status)
		if status >= 200 && status < 300 {
			kind = "invalid_response"
		}
		sourceErr := &weiboSourceError{Kind: kind, HTTPStatus: status}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return nil, sourceErr
	}
	if status < 200 || status >= 300 || weiboDocumentRejected(document) {
		sourceErr := &weiboSourceError{Kind: weiboClassifyError(status, document), HTTPStatus: status}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return nil, sourceErr
	}
	return document, nil
}

func (client *weiboClient) requestCredentialValidation(ctx context.Context, account weiboAccount, sourceErr *weiboSourceError) {
	if client == nil || client.actions == nil || sourceErr == nil || strings.TrimSpace(account.ID) == "" {
		return
	}
	caller, ok := client.actions.(genericLocalActionCaller)
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
	_ = caller.Call(ctx, "thirdparty.account.validate", thirdPartyAccountValidateRequest{
		Platform:    "weibo",
		AccountID:   strings.TrimSpace(account.ID),
		Observation: observation,
		HTTPStatus:  sourceErr.HTTPStatus,
	}, &result)
}

// weiboDocumentRejected 识别 m.weibo.cn 的 ok:0 业务失败响应；没有 ok 字段的文档按正常处理。
func weiboDocumentRejected(document map[string]any) bool {
	value, exists := document["ok"]
	return exists && !boolScalar(value)
}

func requestWeiboAcrossAccounts(ctx context.Context, actions pluginActions, accounts []weiboAccount, rawURL, referer string) (map[string]any, error) {
	client := newWeiboClient(actions)
	var lastError error
	for _, account := range accounts {
		document, err := client.requestJSON(ctx, rawURL, account, referer)
		if err == nil {
			return document, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("没有可用的微博账号")
	}
	return nil, lastError
}

func weiboErrorKind(status int) string {
	switch {
	case status == 401 || status == 403:
		return "auth"
	case status == 432:
		return "session_blocked"
	case status == 412 || status == 418:
		return "risk_control"
	case status == 429:
		return "rate_limit"
	case status >= 500:
		return "server"
	default:
		return "upstream"
	}
}

func weiboClassifyError(status int, document map[string]any) string {
	kind := weiboErrorKind(status)
	if kind == "auth" || kind == "risk_control" || kind == "rate_limit" || kind == "session_blocked" {
		return kind
	}
	if weiboDocumentLooksLikeAuth(document) {
		return "auth"
	}
	if weiboDocumentLooksLikeRisk(document) {
		return "risk_control"
	}
	return kind
}

func weiboDocumentMessage(document map[string]any) string {
	return strings.ToLower(firstText(
		document["msg"], document["message"],
		nestedValue(document, "data", "msg"), nestedValue(document, "data", "message"),
	))
}

func weiboDocumentLooksLikeAuth(document map[string]any) bool {
	message := weiboDocumentMessage(document)
	for _, marker := range []string{"登录", "登陆", "login", "未登录", "expired"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	if data := mapValue(document["data"]); data != nil {
		if _, exists := data["login"]; exists && !boolScalar(data["login"]) {
			return true
		}
	}
	return false
}

func weiboDocumentLooksLikeRisk(document map[string]any) bool {
	message := weiboDocumentMessage(document)
	for _, marker := range []string{"风控", "验证", "captcha", "安全", "频繁", "拦截"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func weiboErrorLogFields(err error) map[string]any {
	fields := map[string]any{}
	var sourceErr *weiboSourceError
	if errors.As(err, &sourceErr) {
		fields["kind"] = firstText(sourceErr.Kind, "upstream")
		if sourceErr.HTTPStatus > 0 {
			fields["http_status"] = sourceErr.HTTPStatus
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

func friendlyWeiboSourceError(label string, err error) string {
	var sourceErr *weiboSourceError
	if !errors.As(err, &sourceErr) {
		if isHTTPActionCapabilityError(err) {
			return label + "：请检查插件 http.request 能力与 http_hosts 配置。"
		}
		if err != nil && strings.Contains(err.Error(), "没有可用的微博账号") {
			return label + "：" + strings.TrimSpace(err.Error()) + "。"
		}
		return label + "。"
	}
	switch sourceErr.Kind {
	case "session_blocked":
		return label + "：H5 会话被拒绝（HTTP 432），可能是 CK 失效或平台风控；请在三方账号页检查 CK。"
	case "risk_control":
		return label + "：微博请求被风控拦截，请稍后再试或重新扫码更新 CK。"
	case "rate_limit":
		return label + "：微博请求过于频繁，请稍后再试。"
	case "auth":
		return label + "：微博账号 CK 已失效，请重新扫码。"
	default:
		if sourceErr.HTTPStatus > 0 {
			return fmt.Sprintf("%s：微博上游请求异常（%s，HTTP %d）。", label, firstText(sourceErr.Kind, "upstream"), sourceErr.HTTPStatus)
		}
		return fmt.Sprintf("%s：微博上游请求异常（%s）。", label, firstText(sourceErr.Kind, "upstream"))
	}
}

func friendlyWeiboError(err error) string {
	if err == nil {
		return "没有找到匹配的微博博主。"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "微博用户信息读取失败。"
	}
	if !strings.HasSuffix(message, "。") {
		message += "。"
	}
	return message
}
