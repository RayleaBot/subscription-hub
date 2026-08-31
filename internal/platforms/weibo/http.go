package weibo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const weiboMobileContainerURL = "https://m.weibo.cn/api/container/getIndex"

const weiboStatusShowURL = "https://m.weibo.cn/statuses/show"

const weiboDetailURL = "https://m.weibo.cn/detail/"

const weiboLongTextURL = "https://m.weibo.cn/statuses/extend"

const weiboWebUserSearchURL = "https://s.weibo.com/user"

const weiboMobileReferer = "https://m.weibo.cn/"

const weiboWebSearchReferer = "https://s.weibo.com/"

const weiboRequestTimeoutSeconds = 10

const weiboSearchTotalTimeout = 15 * time.Second

const weiboDetailTotalTimeout = 15 * time.Second

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
	// auth（CK 失效）同样进冷却：失效会话继续探测会累积平台拦截，
	// 等待账号校验把凭据标记 invalid 后再恢复。
	return err != nil && (err.Kind == "auth" || err.Kind == "risk_control" || err.Kind == "rate_limit" || err.Kind == "session_blocked")
}

func readWeiboAccounts(ctx context.Context, actions plugin.SourceActions) ([]weiboAccount, error) {
	result, err := actions.ThirdPartyAccountRead(ctx, rayleabot.ThirdPartyAccountReadRequest{Platform: "weibo"})
	if err != nil {
		return nil, fmt.Errorf("微博账号读取失败：%w", err)
	}
	accounts := make([]weiboAccount, 0)
	for _, raw := range plugin.SliceValue(result["accounts"]) {
		item := plugin.MapValue(raw)
		cookie := plugin.StringScalar(plugin.NestedValue(item, "cookie", "value"))
		if cookie == "" {
			continue
		}
		accounts = append(accounts, weiboAccount{
			ID:     plugin.StringScalar(item["account_id"]),
			Label:  plugin.StringScalar(item["label"]),
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
		"User-Agent":         plugin.BrowserUserAgent,
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
		if csrf := plugin.CookieField(cookie, "X-CSRF-TOKEN"); csrf != "" {
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
		"User-Agent":      plugin.BrowserUserAgent,
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

func weiboDetailEndpoint(id string) string {
	return weiboDetailURL + url.PathEscape(strings.TrimSpace(id))
}

func weiboLongTextEndpoint(id string) string {
	values := url.Values{}
	values.Set("id", strings.TrimSpace(id))
	return weiboLongTextURL + "?" + values.Encode()
}

type weiboClient struct {
	actions plugin.SourceActions
	now     func() time.Time
}

func newWeiboClient(actions plugin.SourceActions) *weiboClient {
	return &weiboClient{actions: actions, now: time.Now}
}

func (client *weiboClient) requestJSON(ctx context.Context, rawURL string, account weiboAccount, referer string) (map[string]any, error) {
	response, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "GET", URL: rawURL, Headers: weiboRequestHeaders(account.Cookie, referer), TimeoutSeconds: weiboRequestTimeoutSeconds,
	})
	if err != nil {
		return nil, err
	}
	status := int(plugin.IntScalar(response["status_code"]))
	document := plugin.DecodeHTTPDocument(response)
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

func (client *weiboClient) requestHTML(ctx context.Context, rawURL string, account weiboAccount, referer string) (string, error) {
	headers := weiboRequestHeaders(account.Cookie, referer)
	headers["Accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	headers["Sec-Fetch-Dest"] = "document"
	headers["Sec-Fetch-Mode"] = "navigate"
	delete(headers, "X-Requested-With")
	response, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "GET", URL: rawURL, Headers: headers, TimeoutSeconds: weiboRequestTimeoutSeconds,
	})
	if err != nil {
		return "", err
	}
	status := int(plugin.IntScalar(response["status_code"]))
	if status < 200 || status >= 300 {
		sourceErr := &weiboSourceError{Kind: weiboErrorKind(status), HTTPStatus: status}
		client.requestCredentialValidation(ctx, account, sourceErr)
		return "", sourceErr
	}
	body := plugin.StringScalar(response["body_text"])
	if body == "" {
		return "", &weiboSourceError{Kind: "invalid_response", HTTPStatus: status}
	}
	return body, nil
}

func (client *weiboClient) requestCredentialValidation(ctx context.Context, account weiboAccount, sourceErr *weiboSourceError) {
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
		Platform:    "weibo",
		AccountID:   strings.TrimSpace(account.ID),
		Observation: observation,
		HTTPStatus:  sourceErr.HTTPStatus,
	}, &result)
}

// weiboDocumentRejected 识别 m.weibo.cn 的 ok:0 业务失败响应；没有 ok 字段的文档按正常处理。
func weiboDocumentRejected(document map[string]any) bool {
	value, exists := document["ok"]
	return exists && !plugin.BoolScalar(value)
}

func requestWeiboAcrossAccounts(ctx context.Context, actions plugin.SourceActions, accounts []weiboAccount, rawURL, referer string) (map[string]any, error) {
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
	return strings.ToLower(plugin.FirstText(
		document["msg"], document["message"],
		plugin.NestedValue(document, "data", "msg"), plugin.NestedValue(document, "data", "message"),
	))
}

func weiboDocumentLooksLikeAuth(document map[string]any) bool {
	message := weiboDocumentMessage(document)
	for _, marker := range []string{"登录", "登陆", "login", "未登录", "expired"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	if data := plugin.MapValue(document["data"]); data != nil {
		if _, exists := data["login"]; exists && !plugin.BoolScalar(data["login"]) {
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
		fields["kind"] = plugin.FirstText(sourceErr.Kind, "upstream")
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
		if plugin.IsHTTPActionCapabilityError(err) {
			return label + "：请检查插件 http.request 能力与 http_hosts 配置。"
		}
		if err != nil && strings.Contains(err.Error(), "没有可用的微博账号") {
			return label + "：" + strings.TrimSpace(err.Error()) + "。"
		}
		return label + "。"
	}
	switch sourceErr.Kind {
	case "session_blocked":
		return label + "：H5 会话被拒绝（HTTP 432），当前无法确认 CK 状态；已进入退避，请稍后重试或在三方账号页手动检查。"
	case "risk_control":
		return label + "：微博请求被风控拦截，请稍后再试或重新扫码更新 CK。"
	case "rate_limit":
		return label + "：微博请求过于频繁，请稍后再试。"
	case "auth":
		return label + "：微博账号 CK 已失效，请重新扫码。"
	default:
		if sourceErr.HTTPStatus > 0 {
			return fmt.Sprintf("%s：微博上游请求异常（%s，HTTP %d）。", label, plugin.FirstText(sourceErr.Kind, "upstream"), sourceErr.HTTPStatus)
		}
		return fmt.Sprintf("%s：微博上游请求异常（%s）。", label, plugin.FirstText(sourceErr.Kind, "upstream"))
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
