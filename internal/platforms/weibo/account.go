package weibo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const (
	weiboPassportURL = "https://passport.weibo.com"
	weiboSigninURL   = weiboPassportURL + "/sso/signin"
	weiboQRCodeURL   = weiboPassportURL + "/sso/v2/qrcode/image"
	weiboQRCheckURL  = weiboPassportURL + "/sso/v2/qrcode/check"
	weiboRedirectURL = "https://weibo.com/"
	weiboQRVersion   = "20250520"
)

const (
	weiboMobileConfigURL = "https://m.weibo.cn/api/config"
	weiboSideConfigURL   = "https://weibo.com/ajax/side/config"
)

var weiboUserAgent = plugin.BrowserUserAgent

func ValidateAccount(ctx context.Context, actions plugin.SourceActions, cookie string) plugin.AccountValidation {
	cookies := plugin.CookieMapFromHeader(cookie)
	if !weiboHasLoginCookie(cookies) {
		state, message := plugin.CheckedCredential("weibo", plugin.ErrorAuth)
		return plugin.AccountValidation{State: state, Message: message}
	}
	client := plugin.NewAccountHTTPClient(actions)
	profile, err := fetchWeiboAccountProfile(ctx, client, cookies)
	if err != nil {
		kind := plugin.ErrorUpstream
		if typed := plugin.AsAccountError(err); typed != nil {
			kind = typed.Kind
		}
		state, message := plugin.CheckedCredential("weibo", kind)
		if kind != plugin.ErrorAuth && kind != plugin.ErrorExpired {
			if detail := plugin.DiagnosticExcerpt(err.Error(), 160); detail != "" {
				message = detail
			}
		}
		return plugin.AccountValidation{State: state, Message: message}
	}
	return plugin.AccountValidation{State: plugin.CredentialValid, Profile: profile}
}

func fetchWeiboAccountProfile(ctx context.Context, client *http.Client, cookies map[string]string) (plugin.AccountProfile, error) {
	var profile plugin.AccountProfile
	probeErrors := make([]error, 0, 2)
	if configProfile, err := fetchWeiboMobileConfigProfile(ctx, client, cookies); err == nil {
		profile = plugin.MergeAccountProfiles(profile, configProfile)
	} else {
		probeErrors = append(probeErrors, err)
	}
	if configProfile, err := fetchWeiboSideConfigProfile(ctx, client, cookies); err == nil {
		profile = plugin.MergeAccountProfiles(profile, configProfile)
	} else {
		probeErrors = append(probeErrors, err)
	}
	for _, probeErr := range probeErrors {
		if typed := plugin.AsAccountError(probeErr); typed != nil && (typed.Kind == plugin.ErrorAuth || typed.Kind == plugin.ErrorExpired) {
			return plugin.AccountProfile{}, probeErr
		}
	}
	if profile.Empty() {
		if len(probeErrors) > 0 {
			return plugin.AccountProfile{}, probeErrors[0]
		}
		return plugin.AccountProfile{}, plugin.NewAccountError("weibo", plugin.ErrorInvalidResponse, 0, 0, "微博账号资料不可用", nil)
	}
	if strings.TrimSpace(profile.UID) != "" {
		_ = plugin.FollowAccountGet(ctx, client, "https://m.weibo.cn/", weiboProfileHeaders("https://m.weibo.cn/"), cookies)
		if detailProfile, err := fetchWeiboMobileDetailProfile(ctx, client, cookies, profile.UID); err == nil {
			profile = plugin.MergeAccountProfiles(profile, detailProfile)
		}
		if detailProfile, err := fetchWeiboAjaxProfile(ctx, client, cookies, profile.UID); err == nil {
			profile = plugin.MergeAccountProfiles(profile, detailProfile)
		}
	}
	if strings.TrimSpace(profile.AvatarURL) == "" && strings.TrimSpace(profile.UID) != "" {
		if avatar := fetchWeiboAvatarFromMobilePage(ctx, client, profile.UID, cookies); avatar != "" {
			profile.AvatarURL = avatar
		}
	}
	return profile, nil
}

func fetchWeiboMobileConfigProfile(ctx context.Context, client *http.Client, cookies map[string]string) (plugin.AccountProfile, error) {
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := getWeiboJSON(ctx, client, weiboMobileConfigURL, weiboProfileHeaders("https://m.weibo.cn/"), cookies, &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	if login, exists := response.Data["login"].(bool); exists && !login {
		return plugin.AccountProfile{}, weiboCredentialExpiredError(0, http.StatusOK)
	}
	return weiboProfileFromObject(response.Data), nil
}

func fetchWeiboSideConfigProfile(ctx context.Context, client *http.Client, cookies map[string]string) (plugin.AccountProfile, error) {
	var response struct {
		OK   int            `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if err := getWeiboJSON(ctx, client, weiboSideConfigURL, weiboProfileHeaders("https://weibo.com/"), cookies, &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	if response.OK == -100 {
		return plugin.AccountProfile{}, weiboCredentialExpiredError(response.OK, http.StatusOK)
	}
	if response.OK != 0 && response.OK != 1 {
		return plugin.AccountProfile{}, plugin.NewAccountError("weibo", plugin.ErrorUpstream, response.OK, http.StatusOK, "微博账号检查被上游拒绝", nil)
	}
	return weiboProfileFromObject(response.Data), nil
}

func fetchWeiboMobileDetailProfile(ctx context.Context, client *http.Client, cookies map[string]string, uid string) (plugin.AccountProfile, error) {
	values := url.Values{
		"type":        {"uid"},
		"value":       {strings.TrimSpace(uid)},
		"containerid": {"100505" + strings.TrimSpace(uid)},
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := getWeiboJSON(ctx, client, "https://m.weibo.cn/api/container/getIndex?"+values.Encode(), weiboProfileHeaders("https://m.weibo.cn/"), cookies, &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	return weiboProfileFromObject(response.Data), nil
}

func fetchWeiboAjaxProfile(ctx context.Context, client *http.Client, cookies map[string]string, uid string) (plugin.AccountProfile, error) {
	values := url.Values{"uid": {strings.TrimSpace(uid)}}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := getWeiboJSON(ctx, client, "https://weibo.com/ajax/profile/info?"+values.Encode(), weiboProfileHeaders("https://weibo.com/"), cookies, &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	return weiboProfileFromObject(response.Data), nil
}

func getWeiboJSON(ctx context.Context, client *http.Client, rawURL string, headers map[string]string, cookies map[string]string, target any) error {
	if csrf := strings.TrimSpace(cookies["X-CSRF-TOKEN"]); csrf != "" {
		headers["x-csrf-token"] = csrf
	}
	response, err := plugin.GetAccountJSON(ctx, weiboFollowClient(client), rawURL, headers, cookies, target)
	if err != nil {
		if response != nil && (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) {
			return plugin.NewAccountError("weibo", plugin.ClassifyHTTPStatus(response.StatusCode), 0, response.StatusCode, "微博账号检查被上游拒绝", nil)
		}
		if response != nil {
			return plugin.NewAccountError("weibo", plugin.ErrorInvalidResponse, 0, response.StatusCode, "微博账号检查响应格式不正确", err)
		}
		return plugin.NewAccountError("weibo", plugin.ErrorNetwork, 0, 0, "微博账号检查请求失败", err)
	}
	return nil
}

func weiboFollowClient(client *http.Client) *http.Client {
	if client == nil {
		return plugin.NewAccountHTTPClientFollow(nil)
	}
	return plugin.AccountHTTPClientFollowTransport(client.Transport)
}

func fetchWeiboAvatarFromMobilePage(ctx context.Context, client *http.Client, uid string, cookies map[string]string) string {
	body, err := plugin.FetchAccountPageBody(ctx, weiboFollowClient(client),
		"https://m.weibo.cn/u/"+uid, weiboProfileHeaders("https://m.weibo.cn/"), cookies)
	if err != nil {
		return ""
	}
	for _, pattern := range []string{
		`<meta property="og:image" content="`,
		`<meta name="twitter:image" content="`,
		`"avatar_hd":"`,
		`"avatar_large":"`,
		`"profile_image_url":"`,
	} {
		idx := strings.Index(body, pattern)
		if idx < 0 {
			continue
		}
		rest := body[idx+len(pattern):]
		if end := strings.IndexAny(rest, `"<>`); end > 0 {
			candidate := rest[:end]
			if parsed, err := url.Parse(candidate); err == nil && parsed.Scheme == "https" && plugin.HostMatches(parsed.Hostname(), "sinaimg.cn") {
				return candidate
			}
		}
	}
	return ""
}

func weiboCredentialExpiredError(code, httpStatus int) error {
	return plugin.NewAccountError("weibo", plugin.ErrorAuth, code, httpStatus, "微博账号 CK 已失效", nil)
}

func weiboProfileHeaders(referer string) map[string]string {
	return map[string]string{
		"Accept":             "application/json, text/plain, */*",
		"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8",
		"Referer":            referer,
		"User-Agent":         weiboUserAgent,
		"Sec-CH-UA":          `"Chromium";v="134", "Google Chrome";v="134", "Not?A_Brand";v="99"`,
		"Sec-CH-UA-Mobile":   "?0",
		"Sec-CH-UA-Platform": `"Windows"`,
		"Sec-Fetch-Dest":     "empty",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Site":     "same-origin",
		"DNT":                "1",
		"Sec-GPC":            "1",
		"Cache-Control":      "no-cache",
		"X-Requested-With":   "XMLHttpRequest",
	}
}

func weiboProfileFromObject(object map[string]any) plugin.AccountProfile {
	if len(object) == 0 {
		return plugin.AccountProfile{}
	}
	profile := plugin.AccountProfile{
		UID:      plugin.FirstNonEmpty(plugin.JSONStringValue(object["uid"]), plugin.JSONStringValue(object["id"]), plugin.JSONStringValue(object["idstr"])),
		Nickname: plugin.FirstNonEmpty(plugin.JSONStringValue(object["screen_name"]), plugin.JSONStringValue(object["nickname"]), plugin.JSONStringValue(object["name"])),
		AvatarURL: plugin.FirstNonEmpty(
			plugin.JSONStringValue(object["avatar_hd"]),
			plugin.JSONStringValue(object["avatar_large"]),
			plugin.JSONStringValue(object["profile_image_url"]),
			plugin.JSONStringValue(object["avatar"]),
			plugin.JSONStringValue(object["avatar_url"]),
			plugin.JSONStringValue(object["headimgurl"]),
			plugin.JSONStringValue(object["portrait"]),
			plugin.JSONStringValue(object["image"]),
			plugin.JSONStringValue(object["cover_image"]),
		),
	}
	for _, key := range []string{"user", "userInfo", "profile", "cardList", "card_group", "cards", "tabInfo", "newCards"} {
		if nested, ok := object[key].(map[string]any); ok {
			profile = plugin.MergeAccountProfiles(profile, weiboProfileFromObject(nested))
		}
		if arr, ok := object[key].([]any); ok {
			for _, item := range arr {
				if nested, ok := item.(map[string]any); ok {
					profile = plugin.MergeAccountProfiles(profile, weiboProfileFromObject(nested))
				}
			}
		}
	}
	return profile
}

type accountQRProvider struct{}

func NewAccountQRProvider(plugin.QRLoginOptions) plugin.QRLoginProvider { return accountQRProvider{} }

func (accountQRProvider) LoginIDPrefix() string { return "qr" }

func (accountQRProvider) Create(ctx context.Context, actions plugin.SourceActions, now time.Time) (plugin.QRLoginSession, error) {
	cookies := map[string]string{}
	client := plugin.NewAccountHTTPClient(actions)
	signinURL := weiboSigninURL + "?" + url.Values{
		"entry":  {"miniblog"},
		"source": {"miniblog"},
		"url":    {weiboRedirectURL},
	}.Encode()
	if _, err := plugin.GetAccountJSON(ctx, client, signinURL, weiboHeaders(""), cookies, nil); err != nil {
		return plugin.QRLoginSession{}, err
	}
	csrf := strings.TrimSpace(cookies["X-CSRF-TOKEN"])
	if csrf == "" {
		return plugin.QRLoginSession{}, fmt.Errorf("weibo qrcode login missing csrf token")
	}
	var response struct {
		RetCode int    `json:"retcode"`
		Message string `json:"msg"`
		Data    struct {
			QRID  string `json:"qrid"`
			Image string `json:"image"`
		} `json:"data"`
	}
	qrcodeURL := weiboQRCodeURL + "?" + url.Values{"entry": {"miniblog"}, "size": {"180"}}.Encode()
	if _, err := plugin.GetAccountJSON(ctx, client, qrcodeURL, weiboHeaders(csrf), cookies, &response); err != nil {
		return plugin.QRLoginSession{}, err
	}
	if response.RetCode != 20000000 || strings.TrimSpace(response.Data.QRID) == "" {
		return plugin.QRLoginSession{}, fmt.Errorf("weibo qrcode create failed: %s", plugin.FirstNonEmpty(response.Message, "invalid response"))
	}
	return plugin.QRLoginSession{
		Token:     strings.TrimSpace(response.Data.QRID),
		QRCodeURL: weiboScanURL(response.Data.Image, response.Data.QRID),
		ExpiresAt: now.Add(3 * time.Minute),
		State:     plugin.QRLoginStatePendingScan,
		Values:    map[string]string{"csrf": csrf},
		Cookies:   cookies,
	}, nil
}

func (accountQRProvider) Poll(ctx context.Context, actions plugin.SourceActions, session plugin.QRLoginSession, _ time.Time) (plugin.QRLoginSession, error) {
	qrid := strings.TrimSpace(session.Token)
	if qrid == "" {
		return session, plugin.ErrQRLoginSessionNotFound
	}
	cookies := plugin.CloneStringMap(session.Cookies)
	client := plugin.NewAccountHTTPClient(actions)
	var response struct {
		RetCode int    `json:"retcode"`
		Message string `json:"msg"`
		Data    struct {
			URL    string `json:"url"`
			Alt    string `json:"alt"`
			UID    string `json:"uid"`
			Nick   string `json:"nickname"`
			Avatar string `json:"avatar_hd"`
		} `json:"data"`
	}
	checkURL := weiboQRCheckURL + "?" + url.Values{
		"entry":  {"miniblog"},
		"source": {"miniblog"},
		"url":    {weiboRedirectURL},
		"qrid":   {qrid},
		"rid":    {""},
		"ver":    {weiboQRVersion},
	}.Encode()
	if _, err := plugin.GetAccountJSON(ctx, client, checkURL, weiboHeaders(session.Values["csrf"]), cookies, &response); err != nil {
		return session, err
	}
	switch response.RetCode {
	case 20000000:
		redirectHeaders := weiboProfileHeaders("https://weibo.com/")
		if strings.TrimSpace(response.Data.URL) != "" {
			_ = plugin.FollowAccountGet(ctx, client, response.Data.URL, redirectHeaders, cookies)
		}
		if strings.TrimSpace(response.Data.Alt) != "" {
			altURL := "https://login.sina.com.cn/sso/login.php?" + url.Values{
				"entry":      {"miniblog"},
				"alt":        {strings.TrimSpace(response.Data.Alt)},
				"returntype": {"TEXT"},
			}.Encode()
			_ = plugin.FollowAccountGet(ctx, client, altURL, redirectHeaders, cookies)
		}
		if !weiboHasLoginCookie(cookies) {
			return session, fmt.Errorf("weibo qrcode login succeeded without login cookies")
		}
		session.State = plugin.QRLoginStateSucceeded
		if response.Data.UID != "" {
			session.Account = plugin.AccountProfile{UID: response.Data.UID, Nickname: response.Data.Nick, AvatarURL: response.Data.Avatar}
		}
		if profile, err := fetchWeiboAccountProfile(ctx, client, cookies); err == nil {
			session.Account = plugin.MergeAccountProfiles(session.Account, profile)
		}
		session.Cookie = plugin.CookieHeader(cookies)
	case 50114001:
		session.State = plugin.QRLoginStatePendingScan
	case 50114002:
		session.State = plugin.QRLoginStatePendingConfirm
	case 50114004:
		session.State = plugin.QRLoginStateExpired
	default:
		message := strings.TrimSpace(response.Message)
		if strings.Contains(message, "扫") || strings.Contains(message, "scan") {
			session.State = plugin.QRLoginStatePendingConfirm
			break
		}
		return session, fmt.Errorf("weibo qrcode poll retcode %d: %s", response.RetCode, plugin.FirstNonEmpty(message, "invalid response"))
	}
	session.Cookies = cookies
	return session, nil
}

func weiboHeaders(csrf string) map[string]string {
	headers := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Origin":     weiboPassportURL,
		"Referer":    weiboPassportURL + "/sso/signin?entry=miniblog&source=miniblog&url=https://weibo.com/",
		"User-Agent": weiboUserAgent,
	}
	if strings.TrimSpace(csrf) != "" {
		headers["x-csrf-token"] = strings.TrimSpace(csrf)
	}
	return headers
}

func weiboScanURL(imageURL, qrid string) string {
	parsed, err := url.Parse(strings.TrimSpace(imageURL))
	if err == nil {
		if value := strings.TrimSpace(parsed.Query().Get("data")); value != "" {
			return value
		}
	}
	return "https://passport.weibo.cn/signin/qrcode/scan?qr=" + url.QueryEscape(strings.TrimSpace(qrid))
}

func weiboHasLoginCookie(cookies map[string]string) bool {
	for _, name := range []string{"SUB", "SUBP"} {
		if strings.TrimSpace(cookies[name]) != "" {
			return true
		}
	}
	return false
}
