package netease_music

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const (
	neteaseQRCodeCheckURL = "https://music.163.com/weapi/login/qrcode/client/login?csrf_token="
	neteaseAccountURL     = "https://music.163.com/weapi/w/nuser/account/get?csrf_token="
)

var neteaseUserAgent = plugin.BrowserUserAgent

func ValidateAccount(ctx context.Context, actions plugin.SourceActions, cookie string) plugin.AccountValidation {
	cookies := plugin.CookieMapFromHeader(cookie)
	if !hasLoginCookie(cookies) {
		state, message := plugin.CheckedCredential("netease_music", plugin.ErrorAuth)
		return plugin.AccountValidation{State: state, Message: message}
	}
	client := plugin.NewAccountHTTPClient(actions)
	profile, err := fetchNeteaseAccountProfile(ctx, client, cookies)
	if err != nil {
		state, message := plugin.CheckedCredential("netease_music", plugin.ErrorUpstream)
		return plugin.AccountValidation{State: state, Message: message}
	}
	return plugin.AccountValidation{State: plugin.CredentialValid, Profile: profile}
}

type accountQRProvider struct {
	deviceOnce sync.Once
	deviceID   string
}

func NewAccountQRProvider(plugin.QRLoginOptions) plugin.QRLoginProvider { return &accountQRProvider{} }

func (provider *accountQRProvider) LoginIDPrefix() string { return "qr" }

func (provider *accountQRProvider) ensureDeviceID() string {
	provider.deviceOnce.Do(func() {
		id, err := neteaseDeviceID()
		if err != nil {
			id = fmt.Sprintf("%x", time.Now().UnixNano())
		}
		provider.deviceID = id
	})
	return provider.deviceID
}

func (provider *accountQRProvider) Create(ctx context.Context, actions plugin.SourceActions, now time.Time) (plugin.QRLoginSession, error) {
	deviceID := provider.ensureDeviceID()
	nuid, _ := randomHex(16)
	nnid := fmt.Sprintf("%s,%d", nuid, now.UnixMilli())
	nmtid, _ := randomHex(16)
	wnmcid, _ := randomHex(16)
	cookies := map[string]string{
		"os":            "pc",
		"appver":        "2.7.1.198277",
		"osver":         "10",
		"deviceId":      deviceID,
		"WEVNSM":        "1.0.0",
		"WNMCID":        wnmcid,
		"_ntes_nnid":    nnid,
		"_ntes_nuid":    nuid,
		"NMTID":         nmtid,
		"__remember_me": "true",
		"channel":       "",
	}
	client := plugin.NewAccountHTTPClient(actions)
	if err := plugin.FollowAccountGet(ctx, client, "https://music.163.com/", neteaseHeaders(), cookies); err != nil && ctx.Err() != nil {
		return plugin.QRLoginSession{}, ctx.Err()
	}
	csrf := strings.TrimSpace(cookies["__csrf"])
	form, err := neteaseWEAPIFormPayload(map[string]any{"type": 1, "csrf_token": csrf})
	if err != nil {
		return plugin.QRLoginSession{}, err
	}
	var response struct {
		Code   int    `json:"code"`
		UniKey string `json:"unikey"`
	}
	unikeyURL := "https://music.163.com/weapi/login/qrcode/unikey?csrf_token=" + url.QueryEscape(csrf)
	if _, err := plugin.PostAccountFormJSON(ctx, client, unikeyURL, form, neteaseHeaders(), cookies, &response); err != nil {
		return plugin.QRLoginSession{}, err
	}
	if response.Code != 200 || strings.TrimSpace(response.UniKey) == "" {
		return plugin.QRLoginSession{}, fmt.Errorf("netease music qrcode create code %d", response.Code)
	}
	key := strings.TrimSpace(response.UniKey)
	qrcodeURL := "https://music.163.com/login?" + url.Values{
		"codekey": {key},
		"chainId": {neteaseChainID(deviceID, now)},
	}.Encode()
	return plugin.QRLoginSession{
		Token:     key,
		QRCodeURL: qrcodeURL,
		ExpiresAt: now.Add(3 * time.Minute),
		State:     plugin.QRLoginStatePendingScan,
		Cookies:   cookies,
	}, nil
}

func (provider *accountQRProvider) Poll(ctx context.Context, actions plugin.SourceActions, session plugin.QRLoginSession, _ time.Time) (plugin.QRLoginSession, error) {
	key := strings.TrimSpace(session.Token)
	if key == "" {
		return session, plugin.ErrQRLoginSessionNotFound
	}
	cookies := plugin.CloneStringMap(session.Cookies)
	if strings.TrimSpace(cookies["os"]) == "" {
		cookies["os"] = "pc"
	}
	client := plugin.NewAccountHTTPClient(actions)
	var response neteaseLoginResponse
	form, err := neteaseWEAPIFormPayload(map[string]any{
		"type":       1,
		"key":        key,
		"csrf_token": strings.TrimSpace(cookies["__csrf"]),
	})
	if err != nil {
		return session, err
	}
	if _, err := plugin.PostAccountFormJSON(ctx, client, neteaseQRCodeCheckURL, form, neteaseHeaders(), cookies, &response); err != nil {
		return session, err
	}
	switch response.Code {
	case 801:
		session.State = plugin.QRLoginStatePendingScan
	case 802:
		session.State = plugin.QRLoginStatePendingConfirm
		if profile := neteaseProfile(response); !profile.Empty() {
			session.Account = profile
		}
	case 803:
		for cookieKey, value := range plugin.CookieMapFromHeader(response.Cookie) {
			cookies[cookieKey] = value
		}
		if !hasLoginCookie(cookies) {
			return session, fmt.Errorf("netease music qrcode login succeeded without cookies")
		}
		session.State = plugin.QRLoginStateSucceeded
		session.Cookie = plugin.CookieHeader(cookies)
		profile := neteaseProfile(response)
		if profile.Empty() {
			profile = session.Account
		}
		if profile.Empty() {
			if fetched, err := fetchNeteaseAccountProfile(ctx, client, cookies); err == nil {
				profile = fetched
			}
		}
		session.Account = profile
	case 800:
		session.State = plugin.QRLoginStateExpired
	default:
		return session, fmt.Errorf("netease music qrcode poll code %d: %s", response.Code, strings.TrimSpace(response.Message))
	}
	session.Cookies = cookies
	return session, nil
}

func neteaseHeaders() map[string]string {
	return map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"Origin":             "https://music.163.com",
		"Referer":            "https://music.163.com/",
		"User-Agent":         neteaseUserAgent,
		"Sec-CH-UA":          `"Chromium";v="134", "Google Chrome";v="134", "Not?A_Brand";v="99"`,
		"Sec-CH-UA-Mobile":   "?0",
		"Sec-CH-UA-Platform": `"Windows"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-site",
		"DNT":                "1",
		"Sec-GPC":            "1",
		"Cache-Control":      "no-cache",
		"X-Real-IP":          "211.161.244.70",
	}
}

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func neteaseDeviceID() (string, error) {
	var bytes [26]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(bytes[:])), nil
}

func neteaseChainID(deviceID string, now time.Time) string {
	return fmt.Sprintf("v1_%s_web_login_%d", strings.TrimSpace(deviceID), now.UnixMilli())
}

type neteaseLoginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Cookie  string `json:"cookie"`
	Account struct {
		ID int64 `json:"id"`
	} `json:"account"`
	Profile struct {
		UserID    int64  `json:"userId"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"profile"`
}

func neteaseProfile(response neteaseLoginResponse) plugin.AccountProfile {
	uid := response.Profile.UserID
	if uid == 0 {
		uid = response.Account.ID
	}
	if uid == 0 && strings.TrimSpace(response.Profile.Nickname) == "" && strings.TrimSpace(response.Profile.AvatarURL) == "" {
		return plugin.AccountProfile{}
	}
	return plugin.AccountProfile{
		UID:       strconv.FormatInt(uid, 10),
		Nickname:  strings.TrimSpace(response.Profile.Nickname),
		AvatarURL: strings.TrimSpace(response.Profile.AvatarURL),
	}
}

func neteaseWEAPIFormPayload(payload map[string]any) (url.Values, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return neteaseWEAPIForm(string(encoded))
}

func fetchNeteaseAccountProfile(ctx context.Context, client *http.Client, cookies map[string]string) (plugin.AccountProfile, error) {
	form, err := neteaseWEAPIFormPayload(map[string]any{
		"csrf_token": strings.TrimSpace(cookies["__csrf"]),
	})
	if err != nil {
		return plugin.AccountProfile{}, err
	}
	var response neteaseLoginResponse
	if _, err := plugin.PostAccountFormJSON(ctx, client, neteaseAccountURL, form, neteaseHeaders(), cookies, &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	profile := neteaseProfile(response)
	if profile.Empty() {
		return plugin.AccountProfile{}, fmt.Errorf("netease music profile unavailable")
	}
	return profile, nil
}

func hasLoginCookie(cookies map[string]string) bool {
	return strings.TrimSpace(cookies["MUSIC_U"]) != ""
}
