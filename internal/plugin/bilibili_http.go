package plugin

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliDynamicFeedURL = "https://api.bilibili.com/x/polymer/web-dynamic/v1/feed/all"
	bilibiliLiveStatusURL  = "https://api.live.bilibili.com/room/v1/Room/get_status_info_by_uids"
	bilibiliNavURL         = "https://api.bilibili.com/x/web-interface/nav"
	bilibiliRelationURL    = "https://api.bilibili.com/x/relation"
	bilibiliFollowURL      = "https://api.bilibili.com/x/relation/modify"
	bilibiliUserInfoURL    = "https://api.bilibili.com/x/space/wbi/acc/info"
	bilibiliUserSearchURL  = "https://api.bilibili.com/x/web-interface/wbi/search/type"
	bilibiliVideoViewURL   = "https://api.bilibili.com/x/web-interface/view"
	bilibiliOpusDetailURL  = "https://api.bilibili.com/x/polymer/web-dynamic/v1/opus/detail"
	bilibiliDynamicURL     = "https://api.bilibili.com/x/polymer/web-dynamic/v1/detail"
	bilibiliLiveRoomURL    = "https://api.live.bilibili.com/room/v1/Room/get_info"

	bilibiliUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
	bilibiliDMImage   = "V2ViR0wgMS4wIChPcGVuR0wgRVMgMi4wIENocm9taXVtKQ"
	bilibiliDMCover   = "R29vZ2xlIEluYy4gKEludGVsKUFOR0xFIChJbnRlbCwgSW50ZWwoUikgVUhEIEdyYXBoaWNzIERpcmVjdDNEMTEgdnNfNV8wIHBzXzVfMCwgRDNEMTEp"

	bilibiliWBICacheDuration = 12 * time.Hour
)

var (
	bilibiliWBIKeyOrder = []int{
		46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
		27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
		37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
		22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
	}
	bilibiliSecretPattern        = regexp.MustCompile(`(?i)(["']?)(SESSDATA|bili_jct|DedeUserID(?:__ckMd5)?|sid|buvid3|buvid4|ac_time_value|cookie|access[_-]?token|refresh[_-]?token)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^;,\s]+)`)
	bilibiliAuthorizationPattern = regexp.MustCompile(`(?i)(["']?)(authorization)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:bearer|basic)\s+[^;,\s]+|[^;,\s]+)`)
)

type bilibiliAccount struct {
	ID              string
	Label           string
	Cookie          string
	ProfileUID      string
	ProfileNickname string
}

func (account bilibiliAccount) key() string {
	if value := strings.TrimSpace(account.ID); value != "" {
		return value
	}
	digest := sha256.Sum256([]byte(account.Cookie))
	return "cookie-" + hex.EncodeToString(digest[:6])
}

type bilibiliSourceError struct {
	Kind       string
	Message    string
	Code       int
	HTTPStatus int
}

func (err *bilibiliSourceError) Error() string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Message)
}

func (err *bilibiliSourceError) cooldown() bool {
	return err != nil && (err.Kind == "risk_control" || err.Kind == "rate_limit")
}

type bilibiliClient struct {
	actions pluginActions
	now     func() time.Time
}

func newBilibiliClient(actions pluginActions) *bilibiliClient {
	return &bilibiliClient{actions: actions, now: time.Now}
}

func readBilibiliAccounts(ctx context.Context, actions pluginActions) ([]bilibiliAccount, error) {
	result, err := actions.ThirdPartyAccountRead(ctx, rayleabot.ThirdPartyAccountReadRequest{Platform: "bilibili"})
	if err != nil {
		return nil, fmt.Errorf("Bilibili 账号读取失败：%w", err)
	}
	accounts := make([]bilibiliAccount, 0)
	for _, raw := range sliceValue(result["accounts"]) {
		item := mapValue(raw)
		cookie := stringScalar(nestedValue(item, "cookie", "value"))
		if cookie == "" {
			continue
		}
		accounts = append(accounts, bilibiliAccount{
			ID:              stringScalar(item["account_id"]),
			Label:           stringScalar(item["label"]),
			Cookie:          cookie,
			ProfileUID:      stringScalar(nestedValue(item, "profile", "uid")),
			ProfileNickname: stringScalar(nestedValue(item, "profile", "nickname")),
		})
	}
	if len(accounts) == 0 {
		return nil, errors.New("没有可用的 Bilibili 账号 CK，请在 Web 三方账号页面保存账号")
	}
	return accounts, nil
}

func cookieField(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func bilibiliRequestHeaders(cookie, origin, referer string, form bool) map[string]string {
	if strings.TrimSpace(origin) == "" {
		origin = "https://www.bilibili.com"
	}
	if strings.TrimSpace(referer) == "" {
		referer = origin + "/"
	}
	headers := map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"User-Agent":         bilibiliUserAgent,
		"Referer":            referer,
		"Origin":             origin,
		"DNT":                "1",
		"Sec-GPC":            "1",
		"Sec-CH-UA":          `"Chromium";v="134", "Google Chrome";v="134", "Not?A_Brand";v="99"`,
		"Sec-CH-UA-Mobile":   "?0",
		"Sec-CH-UA-Platform": `"Windows"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-site",
	}
	if strings.TrimSpace(cookie) != "" {
		headers["Cookie"] = strings.TrimSpace(cookie)
	}
	if form {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	return headers
}

func bilibiliDeviceQuery() url.Values {
	values := url.Values{}
	values.Set("dm_img_list", "[]")
	values.Set("dm_img_str", bilibiliDMImage)
	values.Set("dm_cover_img_str", bilibiliDMCover)
	values.Set("dm_img_inter", `{"ds":[],"wh":[0,0,0],"of":[0,0,0]}`)
	return values
}

func bilibiliDynamicFeedEndpoint() string {
	values := bilibiliDeviceQuery()
	values.Set("timezone_offset", "-480")
	values.Set("type", "all")
	values.Set("page", "1")
	values.Set("features", "itemOpusStyle,opusBigCover,onlyfansVote,decorationCard,onlyfansAssetsV2,forwardListHidden,ugcDelete")
	return bilibiliDynamicFeedURL + "?" + values.Encode()
}

func bilibiliLiveStatusEndpoint(uids []string) string {
	values := url.Values{}
	for _, uid := range uids {
		if uid = strings.TrimSpace(uid); uid != "" {
			values.Add("uids[]", uid)
		}
	}
	return bilibiliLiveStatusURL + "?" + values.Encode()
}

func extractWBIKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	name := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

func wbiMixinKey(imageKey, subKey string) string {
	raw := strings.TrimSpace(imageKey) + strings.TrimSpace(subKey)
	if len(raw) < len(bilibiliWBIKeyOrder) {
		return ""
	}
	var builder strings.Builder
	for _, index := range bilibiliWBIKeyOrder {
		builder.WriteByte(raw[index])
		if builder.Len() == 32 {
			break
		}
	}
	return builder.String()
}

func sanitizeWBIValue(value string) string {
	return strings.Map(func(char rune) rune {
		if strings.ContainsRune("!'()*", char) {
			return -1
		}
		return char
	}, value)
}

func signWBIURL(rawURL, imageKey, subKey string, timestamp int64) (string, error) {
	mixin := wbiMixinKey(imageKey, subKey)
	if mixin == "" {
		return "", &bilibiliSourceError{Kind: "signature", Message: "WBI 签名密钥不可用"}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	type pair struct{ key, value string }
	pairs := make([]pair, 0)
	for key, values := range parsed.Query() {
		if key == "w_rid" || key == "wts" {
			continue
		}
		for _, value := range values {
			pairs = append(pairs, pair{key: key, value: sanitizeWBIValue(value)})
		}
	}
	pairs = append(pairs, pair{key: "wts", value: strconv.FormatInt(timestamp, 10)})
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	encoded := make([]string, 0, len(pairs))
	for _, item := range pairs {
		encoded = append(encoded, url.QueryEscape(item.key)+"="+url.QueryEscape(item.value))
	}
	query := strings.Join(encoded, "&")
	digest := md5.Sum([]byte(query + mixin))
	parsed.RawQuery = query + "&w_rid=" + hex.EncodeToString(digest[:])
	return parsed.String(), nil
}

func (client *bilibiliClient) signedURL(ctx context.Context, rawURL string, account bilibiliAccount) (string, error) {
	key := "source:bilibili:wbi:" + account.key()
	result, _ := client.actions.KVGet(ctx, key)
	stored, _ := actionStoredValue(result)
	cache := mapValue(stored)
	now := client.now()
	if cache == nil || intScalar(cache["expires_at"]) <= now.Unix() {
		document, err := client.requestJSON(ctx, "GET", bilibiliNavURL, account, false, false, "", false)
		if err != nil {
			return "", err
		}
		imageKey := extractWBIKey(stringScalar(nestedValue(document, "data", "wbi_img", "img_url")))
		subKey := extractWBIKey(stringScalar(nestedValue(document, "data", "wbi_img", "sub_url")))
		if imageKey == "" || subKey == "" {
			return "", &bilibiliSourceError{Kind: "signature", Message: "Bilibili WBI 签名密钥缺失"}
		}
		cache = map[string]any{
			"img_key": imageKey, "sub_key": subKey,
			"expires_at": now.Add(bilibiliWBICacheDuration).Unix(),
		}
		_, _ = client.actions.KVSet(ctx, key, cache)
	}
	return signWBIURL(rawURL, stringScalar(cache["img_key"]), stringScalar(cache["sub_key"]), now.Unix())
}

func (client *bilibiliClient) invalidateWBI(ctx context.Context, account bilibiliAccount) {
	_, _ = client.actions.KVDelete(ctx, "source:bilibili:wbi:"+account.key())
}

func (client *bilibiliClient) requestJSON(
	ctx context.Context,
	method string,
	rawURL string,
	account bilibiliAccount,
	signed bool,
	live bool,
	body string,
	allowSignatureRetry bool,
) (map[string]any, error) {
	requestURL := rawURL
	var err error
	if signed {
		requestURL, err = client.signedURL(ctx, rawURL, account)
		if err != nil {
			return nil, err
		}
	}
	origin := "https://www.bilibili.com"
	if live {
		origin = "https://live.bilibili.com"
	}
	response, err := client.actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: method, URL: requestURL, Headers: bilibiliRequestHeaders(account.Cookie, origin, origin+"/", body != ""),
		TimeoutSeconds: 30, BodyText: body,
	})
	if err != nil {
		return nil, err
	}
	document := decodeBilibiliDocument(response)
	status := int(intScalar(response["status_code"]))
	if document == nil {
		kind := bilibiliErrorKind(status, 0)
		if kind == "upstream" {
			kind = "invalid_response"
		}
		return nil, &bilibiliSourceError{Kind: kind, Message: bilibiliDiagnosticText(response, nil), HTTPStatus: status}
	}
	code := int(intScalar(document["code"]))
	if status < 200 || status >= 300 || code != 0 {
		failure := &bilibiliSourceError{
			Kind: bilibiliErrorKind(status, code), Message: bilibiliDiagnosticText(response, document),
			Code: code, HTTPStatus: status,
		}
		if signed && allowSignatureRetry && body == "" && failure.Kind == "signature" {
			client.invalidateWBI(ctx, account)
			return client.requestJSON(ctx, method, rawURL, account, true, live, body, false)
		}
		return nil, failure
	}
	return document, nil
}

func requestBilibiliAcrossAccounts(
	ctx context.Context,
	actions pluginActions,
	accounts []bilibiliAccount,
	method string,
	rawURL string,
	signed bool,
	live bool,
) (map[string]any, error) {
	client := newBilibiliClient(actions)
	var lastError error
	for _, account := range accounts {
		document, err := client.requestJSON(ctx, method, rawURL, account, signed, live, "", true)
		if err == nil {
			return document, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("没有可用的 Bilibili 账号")
	}
	return nil, lastError
}

func decodeBilibiliDocument(result rayleabot.ActionResult) map[string]any {
	body := stringScalar(result["body_text"])
	if body == "" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil
	}
	return document
}

func bilibiliErrorKind(status, code int) string {
	switch {
	case code == -403 || code == 403:
		return "signature"
	case code == -412 || code == -352 || code == 352 || status == 412:
		return "risk_control"
	case code == -509 || code == -799 || status == 429:
		return "rate_limit"
	case code == -101 || code == -102 || code == -658 || status == 401 || status == 403:
		return "auth"
	case status >= 500:
		return "server"
	default:
		return "upstream"
	}
}

func bilibiliDiagnosticText(response rayleabot.ActionResult, document map[string]any) string {
	status := int(intScalar(response["status_code"]))
	parts := []string{fmt.Sprintf("HTTP %d", status)}
	if document != nil {
		if code := intScalar(document["code"]); code != 0 {
			parts = append(parts, fmt.Sprintf("Bilibili code %d", code))
		}
		if reason := diagnosticExcerpt(stringScalar(document["message"]), 240); reason != "" {
			parts = append(parts, "原始原因："+reason)
		}
	} else if body := diagnosticExcerpt(stringScalar(response["body_text"]), 240); body != "" {
		parts = append(parts, "原始原因："+body)
	}
	if trace := bilibiliTraceID(mapValue(response["headers"])); trace != "" {
		parts = append(parts, "请求标识："+trace)
	}
	return "诊断信息：" + strings.Join(parts, "；") + "。"
}

func bilibiliTraceID(headers map[string]any) string {
	for key, value := range headers {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "bili-trace-id", "request-id", "trace-id", "x-bili-trace-id", "x-request-id", "x-trace-id":
			return diagnosticExcerpt(stringScalar(value), 120)
		}
	}
	return ""
}

func diagnosticExcerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	value = bilibiliAuthorizationPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = bilibiliSecretPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = strings.TrimRight(strings.TrimSpace(value), "。.;； ")
	if len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func friendlyBilibiliSourceError(label string, err error) string {
	var sourceErr *bilibiliSourceError
	if !errors.As(err, &sourceErr) {
		if isHTTPActionCapabilityError(err) {
			return label + "：请检查插件 http.request 能力与 http_hosts 配置。"
		}
		return label + "。"
	}
	switch sourceErr.Kind {
	case "risk_control":
		return label + "：Bilibili 请求被风控拦截，请稍后再试或重新扫码更新 CK。" + sourceErr.Message
	case "rate_limit":
		return label + "：Bilibili 请求过于频繁，请稍后再试。" + sourceErr.Message
	case "auth":
		return label + "：Bilibili 账号 CK 已失效，请重新扫码。" + sourceErr.Message
	case "signature":
		return label + "：Bilibili WBI 签名不可用。" + sourceErr.Message
	default:
		return label + "：" + sourceErr.Message
	}
}

func isHTTPActionCapabilityError(err error) bool {
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		if actionErr.Code == "plugin.capability_violation" {
			return true
		}
	}
	text := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(text, "capability") || strings.Contains(text, "http_hosts")
}
