package bilibili

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const (
	bilibiliAccountNavURL        = "https://api.bilibili.com/x/web-interface/nav"
	bilibiliAccountQRCodeURL     = "https://passport.bilibili.com/x/passport-login/web/qrcode/generate?source=main-fe-header"
	bilibiliAccountQRCodePollURL = "https://passport.bilibili.com/x/passport-login/web/qrcode/poll"
)

var bilibiliAccountHeaders = map[string]string{
	"Accept":             "application/json, text/plain, */*",
	"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
	"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36",
	"Referer":            "https://www.bilibili.com/",
	"Origin":             "https://www.bilibili.com",
	"DNT":                "1",
	"Sec-GPC":            "1",
	"Sec-CH-UA":          `"Chromium";v="134", "Google Chrome";v="134", "Not?A_Brand";v="99"`,
	"Sec-CH-UA-Mobile":   "?0",
	"Sec-CH-UA-Platform": `"Windows"`,
	"Sec-Fetch-Dest":     "empty",
	"Sec-Fetch-Mode":     "cors",
	"Sec-Fetch-Site":     "same-site",
}

func ValidateAccount(ctx context.Context, actions plugin.SourceActions, cookie string) plugin.AccountValidation {
	cookie = strings.TrimSpace(cookie)
	if bilibiliCookieField(cookie, "SESSDATA") == "" {
		return plugin.AccountValidation{State: plugin.CredentialInvalid, Message: "Bilibili 账号 CK 缺少 SESSDATA，请重新扫码"}
	}
	headers := make(map[string]string, len(bilibiliAccountHeaders)+1)
	for key, value := range bilibiliAccountHeaders {
		headers[key] = value
	}
	headers["Cookie"] = cookie
	client := plugin.NewAccountHTTPClient(actions)
	var document struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			IsLogin *bool  `json:"isLogin"`
			Mid     any    `json:"mid"`
			UName   string `json:"uname"`
			Face    string `json:"face"`
		} `json:"data"`
	}
	response, err := plugin.GetAccountJSON(ctx, client, bilibiliAccountNavURL, headers, nil, &document)
	if err != nil {
		kind := plugin.ErrorUpstream
		if response != nil && response.StatusCode == http.StatusUnauthorized {
			kind = plugin.ErrorAuth
		}
		state, message := plugin.CheckedCredential("bilibili", kind)
		return plugin.AccountValidation{State: state, Message: message}
	}
	if document.Code != 0 || document.Data.IsLogin == nil || !*document.Data.IsLogin {
		kind := plugin.ErrorUpstream
		switch document.Code {
		case -101, -102, -658:
			kind = plugin.ErrorAuth
		case -352, -412:
			kind = plugin.ErrorRiskControl
		case 0:
			if document.Data.IsLogin != nil && !*document.Data.IsLogin {
				kind = plugin.ErrorAuth
			}
		}
		state, message := plugin.CheckedCredential("bilibili", kind)
		return plugin.AccountValidation{State: state, Message: message}
	}
	uid := plugin.StringScalar(document.Data.Mid)
	if uid == "" {
		state, message := plugin.CheckedCredential("bilibili", plugin.ErrorInvalidResponse)
		return plugin.AccountValidation{State: state, Message: message}
	}
	return plugin.AccountValidation{
		State: plugin.CredentialValid,
		Profile: plugin.AccountProfile{
			UID:       uid,
			Nickname:  strings.TrimSpace(document.Data.UName),
			AvatarURL: normalizeBilibiliURL(document.Data.Face),
		},
	}
}

type accountQRProvider struct{}

func NewAccountQRProvider(plugin.QRLoginOptions) plugin.QRLoginProvider { return accountQRProvider{} }

func (accountQRProvider) LoginIDPrefix() string { return "qr" }

func (accountQRProvider) Create(ctx context.Context, actions plugin.SourceActions, now time.Time) (plugin.QRLoginSession, error) {
	client := plugin.NewAccountHTTPClient(actions)
	var document struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
	}
	if _, err := plugin.GetAccountJSON(ctx, client, bilibiliAccountQRCodeURL, bilibiliAccountHeaders, nil, &document); err != nil {
		return plugin.QRLoginSession{}, fmt.Errorf("bilibili qr generate: %w", err)
	}
	if document.Code != 0 || strings.TrimSpace(document.Data.URL) == "" || strings.TrimSpace(document.Data.QRCodeKey) == "" {
		message := plugin.FirstNonEmpty(document.Message, "二维码创建失败")
		return plugin.QRLoginSession{}, fmt.Errorf("bilibili qr generate: %s", message)
	}
	expiresAt := now.UTC().Add(3 * time.Minute)
	return plugin.QRLoginSession{
		QRCodeURL: strings.TrimSpace(document.Data.URL),
		ExpiresAt: expiresAt,
		State:     plugin.QRLoginStatePendingScan,
		Values:    map[string]string{"qrcode_key": strings.TrimSpace(document.Data.QRCodeKey)},
	}, nil
}

func (accountQRProvider) Poll(ctx context.Context, actions plugin.SourceActions, session plugin.QRLoginSession, now time.Time) (plugin.QRLoginSession, error) {
	qrcodeKey := strings.TrimSpace(session.Values["qrcode_key"])
	if qrcodeKey == "" {
		return session, plugin.ErrQRLoginSessionNotFound
	}
	values := url.Values{"qrcode_key": {qrcodeKey}, "source": {"main-fe-header"}}
	client := plugin.NewAccountHTTPClient(actions)
	var document struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code         int    `json:"code"`
			Message      string `json:"message"`
			URL          string `json:"url"`
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	response, err := plugin.GetAccountJSON(ctx, client, bilibiliAccountQRCodePollURL+"?"+values.Encode(), bilibiliAccountHeaders, nil, &document)
	if err != nil {
		return session, fmt.Errorf("bilibili qr poll: %w", err)
	}
	if document.Code != 0 {
		return session, fmt.Errorf("bilibili qr poll: %s", plugin.FirstNonEmpty(document.Message, "二维码状态读取失败"))
	}
	switch document.Data.Code {
	case 86101:
		session.State = plugin.QRLoginStatePendingScan
	case 86090:
		session.State = plugin.QRLoginStatePendingConfirm
	case 86038:
		session.State = plugin.QRLoginStateExpired
	case 0:
		cookie, err := bilibiliCookieFromLogin(document.Data.URL, document.Data.RefreshToken, response)
		if err != nil {
			return session, err
		}
		profile := plugin.AccountProfile{}
		if validation := ValidateAccount(ctx, actions, cookie); validation.State == plugin.CredentialValid {
			profile = validation.Profile
		}
		if profile.UID == "" {
			profile.UID = bilibiliCookieField(cookie, "DedeUserID")
		}
		session.State = plugin.QRLoginStateSucceeded
		session.Cookie = cookie
		session.Account = profile
	default:
		return session, fmt.Errorf("bilibili qr poll code %d: %s", document.Data.Code, plugin.FirstNonEmpty(document.Data.Message, "二维码状态读取失败"))
	}
	return session, nil
}

func bilibiliCookieFromLogin(rawURL, refreshToken string, response *http.Response) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	values := map[string]string{}
	if err == nil {
		query := parsed.Query()
		for _, key := range []string{"SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "sid"} {
			if value := strings.TrimSpace(query.Get(key)); value != "" {
				values[key] = value
			}
		}
	}
	if response != nil {
		for _, item := range response.Cookies() {
			if item == nil || item.MaxAge < 0 {
				continue
			}
			name := strings.TrimSpace(item.Name)
			value := strings.TrimSpace(item.Value)
			if name != "" && value != "" {
				values[name] = value
			}
		}
	}
	if strings.TrimSpace(refreshToken) != "" {
		values["ac_time_value"] = strings.TrimSpace(refreshToken)
	}
	for _, key := range []string{"SESSDATA", "bili_jct", "DedeUserID"} {
		if strings.TrimSpace(values[key]) == "" {
			return "", fmt.Errorf("bilibili login missing %s", key)
		}
	}
	return mergeBilibiliCookies(values), nil
}

func mergeBilibiliCookies(updates map[string]string) string {
	parts := make([]string, 0, len(updates))
	for _, key := range []string{"SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "sid", "ac_time_value"} {
		if value := strings.TrimSpace(updates[key]); value != "" {
			parts = append(parts, key+"="+value)
			delete(updates, key)
		}
	}
	for key, value := range updates {
		if value = strings.TrimSpace(value); key != "" && value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + ";"
}

func bilibiliCookieField(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeBilibiliURL(value string) string {
	text := strings.TrimSpace(value)
	if strings.HasPrefix(text, "//") {
		return "https:" + text
	}
	return text
}
