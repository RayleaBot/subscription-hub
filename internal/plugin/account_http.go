package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	maxAccountResponseBytes = 4 << 20
	accountRequestTimeout   = 20
)

type ErrorKind string

const (
	ErrorAuth            ErrorKind = "auth"
	ErrorCSRF            ErrorKind = "csrf"
	ErrorRiskControl     ErrorKind = "risk_control"
	ErrorCaptcha         ErrorKind = "captcha"
	ErrorRateLimit       ErrorKind = "rate_limit"
	ErrorSignature       ErrorKind = "signature"
	ErrorNotFound        ErrorKind = "not_found"
	ErrorBadRequest      ErrorKind = "bad_request"
	ErrorServer          ErrorKind = "server"
	ErrorInvalidResponse ErrorKind = "invalid_response"
	ErrorUpstream        ErrorKind = "upstream"
	ErrorNetwork         ErrorKind = "network"
	ErrorExpired         ErrorKind = "expired"
)

type AccountError struct {
	Platform   string
	Kind       ErrorKind
	Code       int
	HTTPStatus int
	Message    string
	Body       string
	Err        error
}

func (e *AccountError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{e.Platform, string(e.Kind)}
	if e.Code != 0 {
		parts = append(parts, "code "+strconv.Itoa(e.Code))
	}
	if e.HTTPStatus != 0 {
		parts = append(parts, "HTTP "+strconv.Itoa(e.HTTPStatus))
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, strings.TrimSpace(e.Message))
	}
	if e.Err != nil && strings.TrimSpace(e.Err.Error()) != "" {
		parts = append(parts, e.Err.Error())
	}
	return strings.Join(parts, ": ")
}

func (e *AccountError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func AsAccountError(err error) *AccountError {
	var target *AccountError
	if errors.As(err, &target) {
		return target
	}
	return nil
}

func NewAccountError(platform string, kind ErrorKind, code int, httpStatus int, message string, err error) *AccountError {
	return &AccountError{
		Platform:   platform,
		Kind:       kind,
		Code:       code,
		HTTPStatus: httpStatus,
		Message:    strings.TrimSpace(message),
		Err:        err,
	}
}

func ClassifyHTTPStatus(status int) ErrorKind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorAuth
	case http.StatusBadRequest:
		return ErrorBadRequest
	case http.StatusNotFound:
		return ErrorNotFound
	case http.StatusTooManyRequests:
		return ErrorRateLimit
	default:
		if status >= 500 {
			return ErrorServer
		}
		return ErrorUpstream
	}
}

// CheckedCredential maps one observed check outcome to a persisted credential
// state. Only an explicit authentication rejection invalidates credentials;
// transport and risk-control uncertainty keeps the previous state unknown.
func CheckedCredential(platform string, kind ErrorKind) (string, string) {
	if kind == "" {
		return CredentialValid, ""
	}
	label := platform
	switch platform {
	case "bilibili":
		label = "Bilibili"
	case "weibo":
		label = "微博"
	case "douyin":
		label = "抖音"
	case "netease_music":
		label = "网易云音乐"
	}
	switch kind {
	case ErrorAuth, ErrorExpired:
		if platform == "bilibili" {
			label += " "
		}
		return CredentialInvalid, label + "账号 CK 已失效，请重新扫码"
	case ErrorRiskControl, ErrorCaptcha:
		return CredentialUnknown, label + " CK 检查受到平台风控限制，请稍后重试"
	case ErrorRateLimit:
		return CredentialUnknown, label + " CK 检查触发频率限制，请稍后重试"
	default:
		return CredentialUnknown, label + " CK 状态暂时无法确认，请稍后重试"
	}
}

// actionTransport performs HTTP requests through the authenticated plugin
// http.request action so account flows share the host's HTTPS, DNS,
// redirect, SSRF, and response-size boundaries.
type actionTransport struct {
	actions SourceActions
}

func (transport actionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.actions == nil {
		return nil, errors.New("account http transport is unavailable")
	}
	body, err := readRequestBody(request)
	if err != nil {
		return nil, err
	}
	input := rayleabot.HTTPRequest{
		Method:         request.Method,
		URL:            request.URL.String(),
		Headers:        firstValueHeaders(request.Header),
		TimeoutSeconds: accountRequestTimeout,
	}
	if len(body) > 0 {
		if utf8.Valid(body) {
			input.BodyText = string(body)
		} else {
			input.BodyBase64 = base64.StdEncoding.EncodeToString(body)
		}
	}
	result, err := transport.actions.HTTPRequest(request.Context(), input)
	if err != nil {
		return nil, err
	}
	responseBody, err := responseBodyBytes(result)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	for key, value := range MapValue(result["headers"]) {
		headers.Set(key, StringScalar(value))
	}
	for _, value := range SliceValue(result["set_cookies"]) {
		headers.Add("Set-Cookie", StringScalar(value))
	}
	return &http.Response{
		StatusCode:    int(IntScalar(result["status_code"])),
		Status:        strconv.Itoa(int(IntScalar(result["status_code"]))),
		Header:        headers,
		Body:          io.NopCloser(bytes.NewReader(responseBody)),
		ContentLength: int64(len(responseBody)),
		Request:       request,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
	}, nil
}

func readRequestBody(request *http.Request) ([]byte, error) {
	if request == nil || request.Body == nil {
		return nil, nil
	}
	defer func(release func() error) { _ = release() }(request.Body.Close)
	return io.ReadAll(io.LimitReader(request.Body, maxAccountResponseBytes))
}

func firstValueHeaders(header http.Header) map[string]string {
	result := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}

func responseBodyBytes(result rayleabot.ActionResult) ([]byte, error) {
	if encoded := StringScalar(result["body_base64"]); encoded != "" {
		body, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("account response body is invalid")
		}
		return body, nil
	}
	if text, exists := result["body_text"].(string); exists {
		return []byte(text), nil
	}
	return nil, nil
}

func NewAccountHTTPClient(actions SourceActions) *http.Client {
	return &http.Client{
		Transport: actionTransport{actions: actions},
		Timeout:   accountRequestTimeout * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewAccountHTTPClientFollow follows redirects normally. Use it for endpoints
// that issue 302 redirects during login.
func NewAccountHTTPClientFollow(actions SourceActions) *http.Client {
	return &http.Client{
		Transport: actionTransport{actions: actions},
		Timeout:   accountRequestTimeout * time.Second,
	}
}

func FetchAccountPageBody(ctx context.Context, client *http.Client, rawURL string, headers map[string]string, cookies map[string]string) (string, error) {
	safeURL, err := ValidateAccountURL(rawURL)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL, nil)
	if err != nil {
		return "", err
	}
	ApplyAccountHeaders(request, headers, cookies)
	response, err := accountHTTPDo(client, request)
	if err != nil {
		return "", err
	}
	defer func(release func() error) { _ = release() }(response.Body.Close)
	MergeResponseCookies(cookies, response)
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAccountResponseBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func GetAccountJSON(ctx context.Context, client *http.Client, rawURL string, headers map[string]string, cookies map[string]string, target any) (*http.Response, error) {
	safeURL, err := ValidateAccountURL(rawURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL, nil)
	if err != nil {
		return nil, err
	}
	ApplyAccountHeaders(request, headers, cookies)
	return accountJSONResponse(client, request, cookies, target)
}

func PostAccountFormJSON(ctx context.Context, client *http.Client, rawURL string, form url.Values, headers map[string]string, cookies map[string]string, target any) (*http.Response, error) {
	safeURL, err := ValidateAccountURL(rawURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, safeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ApplyAccountHeaders(request, headers, cookies)
	return accountJSONResponse(client, request, cookies, target)
}

func accountJSONResponse(client *http.Client, request *http.Request, cookies map[string]string, target any) (*http.Response, error) {
	response, err := accountHTTPDo(client, request)
	if err != nil {
		return nil, err
	}
	defer func(release func() error) { _ = release() }(response.Body.Close)
	MergeResponseCookies(cookies, response)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, fmt.Errorf("account request http %d", response.StatusCode)
	}
	if target != nil {
		decoder := json.NewDecoder(io.LimitReader(response.Body, maxAccountResponseBytes))
		if err := decoder.Decode(target); err != nil {
			return response, err
		}
	}
	return response, nil
}

func FollowAccountGet(ctx context.Context, client *http.Client, rawURL string, headers map[string]string, cookies map[string]string) error {
	current := strings.TrimSpace(rawURL)
	client = withManualAccountRedirects(client)
	for i := 0; i < 8; i++ {
		safeURL, err := ValidateAccountURL(current)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL, nil)
		if err != nil {
			return err
		}
		ApplyAccountHeaders(request, headers, cookies)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		MergeResponseCookies(cookies, response)
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxAccountResponseBytes))
		_ = response.Body.Close()
		if response.StatusCode < 300 || response.StatusCode >= 400 {
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return fmt.Errorf("account follow http %d", response.StatusCode)
			}
			return nil
		}
		location := strings.TrimSpace(response.Header.Get("Location"))
		if location == "" {
			return fmt.Errorf("account redirect missing location")
		}
		next, err := url.Parse(location)
		if err != nil {
			return err
		}
		base, err := url.Parse(current)
		if err != nil {
			return err
		}
		current = base.ResolveReference(next).String()
	}
	return fmt.Errorf("account redirect limit exceeded")
}

func accountHTTPDo(client *http.Client, request *http.Request) (*http.Response, error) {
	return withAccountRedirectGuard(client).Do(request)
}

func withAccountRedirectGuard(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Transport: actionTransport{}}
	}
	guarded := *client
	originalCheckRedirect := client.CheckRedirect
	guarded.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if _, err := ValidateAccountURL(request.URL.String()); err != nil {
			return err
		}
		if len(via) >= 8 {
			return fmt.Errorf("account request redirect limit exceeded")
		}
		if originalCheckRedirect != nil {
			return originalCheckRedirect(request, via)
		}
		return nil
	}
	return &guarded
}

func withManualAccountRedirects(client *http.Client) *http.Client {
	guarded := withAccountRedirectGuard(client)
	guarded.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		if _, err := ValidateAccountURL(request.URL.String()); err != nil {
			return err
		}
		return http.ErrUseLastResponse
	}
	return guarded
}

var allowedAccountHostSuffixes = []string{
	"amemv.com",
	"bilibili.com",
	"douyin.com",
	"douyinpic.com",
	"hdslb.com",
	"music.163.com",
	"sina.com.cn",
	"sinaimg.cn",
	"weibo.cn",
	"weibo.com",
}

func ValidateAccountURL(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return "", errors.New("account request URL is invalid")
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("account request URL is not allowed")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || isLocalOrPrivateHost(host) || !isAllowedAccountHost(host) {
		return "", fmt.Errorf("account request URL host %q is not allowed", host)
	}
	return parsed.String(), nil
}

func isAllowedAccountHost(host string) bool {
	return HostMatches(host, allowedAccountHostSuffixes...)
}

func isLocalOrPrivateHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() ||
			ip.IsPrivate() ||
			ip.IsUnspecified() ||
			ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() ||
			ip.IsMulticast()
	}
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func HostMatches(host string, suffixes ...string) bool {
	normalizedHost := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if normalizedHost == "" {
		return false
	}
	for _, suffix := range suffixes {
		normalizedSuffix := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(suffix)), ".")
		normalizedSuffix = strings.TrimPrefix(normalizedSuffix, ".")
		if normalizedSuffix != "" && (normalizedHost == normalizedSuffix || strings.HasSuffix(normalizedHost, "."+normalizedSuffix)) {
			return true
		}
	}
	return false
}

func ApplyAccountHeaders(request *http.Request, headers map[string]string, cookies map[string]string) {
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			request.Header.Set(key, value)
		}
	}
	if header := CookieHeader(cookies); header != "" {
		request.Header.Set("Cookie", header)
	}
}

func MergeResponseCookies(cookies map[string]string, response *http.Response) {
	if cookies == nil || response == nil {
		return
	}
	for _, cookie := range response.Cookies() {
		name := strings.TrimSpace(cookie.Name)
		if name != "" {
			cookies[name] = cookie.Value
		}
	}
}

func CookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	keys := make([]string, 0, len(cookies))
	for key, value := range cookies {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+cookies[key])
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + ";"
}

func CookieMapFromHeader(header string) map[string]string {
	values := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name != "" && value != "" {
			values[name] = value
		}
	}
	return values
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func CloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
