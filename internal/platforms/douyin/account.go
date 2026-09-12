package douyin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const (
	douyinSelfProfileURL = "https://www.douyin.com/aweme/v1/web/user/profile/self/?aid=6383"

	douyinBrowserLoginURL      = "https://www.douyin.com/login_page?service=https%3A%2F%2Fwww.douyin.com%2F"
	douyinBrowserLoginHost     = "login.douyin.com"
	douyinBrowserQRCodePath    = "/passport/web/get_qrcode/"
	douyinBrowserQRConnectPath = "/passport/web/check_qrconnect/"
	douyinBrowserHomeURL       = "https://www.douyin.com/"
	douyinBrowserSettleDelay   = 8 * time.Second
	douyinBrowserSettleTimeout = 15 * time.Second
	douyinBrowserCreateTimeout = 25 * time.Second
	douyinBrowserResponseLimit = 256 << 10
)

func ValidateAccount(ctx context.Context, actions plugin.SourceActions, cookie string) plugin.AccountValidation {
	if !douyinHasLoginCookie(cookie) {
		state, message := plugin.CheckedCredential("douyin", plugin.ErrorAuth)
		return plugin.AccountValidation{State: state, Message: message}
	}
	client := newDouyinClient(actions)
	document, err := client.requestJSON(ctx, douyinSelfProfileURL, douyinAccount{Cookie: cookie}, douyinWebReferer)
	if err != nil {
		kind := plugin.ErrorUpstream
		if sourceErr, ok := err.(*douyinSourceError); ok {
			switch sourceErr.Kind {
			case "auth":
				kind = plugin.ErrorAuth
			case "risk_control", "session_blocked":
				kind = plugin.ErrorRiskControl
			case "rate_limit":
				kind = plugin.ErrorRateLimit
			}
		}
		state, message := plugin.CheckedCredential("douyin", kind)
		return plugin.AccountValidation{State: state, Message: message}
	}
	user := plugin.MapValue(document["user"])
	if user == nil {
		state, message := plugin.CheckedCredential("douyin", plugin.ErrorInvalidResponse)
		return plugin.AccountValidation{State: state, Message: message}
	}
	profile := plugin.AccountProfile{
		UID:      plugin.FirstNonEmpty(plugin.JSONStringValue(user["unique_id"]), plugin.JSONStringValue(user["short_id"]), plugin.JSONStringValue(user["uid"])),
		Nickname: plugin.StringScalar(user["nickname"]),
	}
	if avatar := plugin.MapValue(user["avatar_medium"]); avatar != nil {
		profile.AvatarURL = firstDouyinAvatarURL(avatar)
	}
	if profile.AvatarURL == "" {
		if avatar := plugin.MapValue(user["avatar_thumb"]); avatar != nil {
			profile.AvatarURL = firstDouyinAvatarURL(avatar)
		}
	}
	return plugin.AccountValidation{State: plugin.CredentialValid, Profile: profile}
}

func firstDouyinAvatarURL(avatar map[string]any) string {
	for _, raw := range plugin.SliceValue(avatar["url_list"]) {
		if value := plugin.StringScalar(raw); value != "" {
			return value
		}
	}
	return ""
}

type accountQRProvider struct {
	mode      string
	remoteURL string

	mu       sync.Mutex
	sessions map[string]*douyinBrowserRuntime
}

type douyinBrowserRuntime struct {
	session   *plugin.BrowserSession
	capture   *plugin.BrowserResponseCapture
	token     string
	qrcodeURL string
	expiresAt time.Time
	pollMode  string
	cookies   map[string]string

	lastState  string
	blockCount int
	pollSeq    uint64
}

const (
	douyinPollCaptured    = "captured"
	douyinPollActiveToken = "active_token"
)

func NewAccountQRProvider(options plugin.QRLoginOptions) plugin.QRLoginProvider {
	return &accountQRProvider{
		mode:      plugin.NormalizeAccountBrowserMode(options.BrowserMode),
		remoteURL: strings.TrimSpace(options.BrowserRemoteURL),
		sessions:  map[string]*douyinBrowserRuntime{},
	}
}

func (provider *accountQRProvider) LoginIDPrefix() string { return "qr" }

func (provider *accountQRProvider) Create(ctx context.Context, actions plugin.SourceActions, now time.Time) (plugin.QRLoginSession, error) {
	session, err := plugin.LaunchBrowserSession(ctx, actions, rayleabot.BrowserLaunchRequest{Profile: "douyin-login", Mode: provider.mode, RemoteDebuggingURL: provider.remoteURL, LifetimeSeconds: 180})
	if err != nil {
		return plugin.QRLoginSession{}, err
	}
	createCtx, cancelCreate := context.WithTimeout(ctx, douyinBrowserCreateTimeout)
	defer cancelCreate()
	result, runtime, err := provider.createWithBrowser(createCtx, session, now)
	if err != nil {
		session.Close(createCtx, actions)
		return plugin.QRLoginSession{}, err
	}
	provider.mu.Lock()
	provider.sessions[result.Token] = runtime
	provider.mu.Unlock()
	return result, nil
}

func (provider *accountQRProvider) createWithBrowser(ctx context.Context, session *plugin.BrowserSession, now time.Time) (plugin.QRLoginSession, *douyinBrowserRuntime, error) {
	if err := plugin.OverrideBrowserUserAgent(ctx, session); err != nil {
		return plugin.QRLoginSession{}, nil, fmt.Errorf("douyin browser user agent setup failed: %w", err)
	}
	if err := clearDouyinLoginCookies(ctx, session); err != nil {
		return plugin.QRLoginSession{}, nil, fmt.Errorf("douyin browser profile reset failed: %w", err)
	}
	capture := plugin.WatchBrowserResponses(session.Context(), classifyDouyinQRNetwork, douyinBrowserResponseLimit)
	if err := session.RunTimeout(ctx, 12*time.Second,
		network.Enable(),
		emulation.SetTimezoneOverride("Asia/Shanghai"),
		emulation.SetFocusEmulationEnabled(true),
		chromedp.Navigate(douyinBrowserLoginURL),
		chromedp.WaitReady("body"),
	); err != nil {
		return plugin.QRLoginSession{}, nil, fmt.Errorf("douyin browser navigation failed")
	}
	result, pollMode, err := waitDouyinBrowserQRCode(ctx, session, capture, now)
	if err != nil {
		return plugin.QRLoginSession{}, nil, err
	}
	cookies, err := session.Cookies(ctx, []string{
		"https://www.douyin.com/", "https://login.douyin.com/", "https://sso.douyin.com/",
	})
	if err != nil {
		return plugin.QRLoginSession{}, nil, fmt.Errorf("douyin browser cookie read failed")
	}
	runtime := &douyinBrowserRuntime{
		session:   session,
		capture:   capture,
		token:     result.Token,
		qrcodeURL: result.QRCodeURL,
		expiresAt: result.ExpiresAt,
		pollMode:  pollMode,
		cookies:   cloneDouyinCookies(cookies),
		lastState: plugin.QRLoginStatePendingScan,
	}
	return plugin.QRLoginSession{
		Token:     result.Token,
		QRCodeURL: result.QRCodeURL,
		ExpiresAt: result.ExpiresAt,
		State:     plugin.QRLoginStatePendingScan,
		Cookies:   cloneDouyinCookies(cookies),
	}, runtime, nil
}

func (provider *accountQRProvider) Poll(ctx context.Context, actions plugin.SourceActions, session plugin.QRLoginSession, now time.Time) (plugin.QRLoginSession, error) {
	token := strings.TrimSpace(session.Token)
	provider.mu.Lock()
	runtime := provider.sessions[token]
	provider.mu.Unlock()
	if runtime == nil {
		return session, plugin.ErrQRLoginSessionNotFound
	}
	if !runtime.expiresAt.IsZero() && now.UTC().After(runtime.expiresAt) {
		provider.closeRuntime(ctx, actions, token, runtime)
		session.State = plugin.QRLoginStateExpired
		return session, nil
	}
	cookies, err := runtime.session.Cookies(ctx, []string{
		"https://www.douyin.com/", "https://login.douyin.com/", "https://sso.douyin.com/", "https://api.amemv.com/",
	})
	if err != nil {
		if ctx.Err() != nil {
			return session, ctx.Err()
		}
		provider.closeRuntime(ctx, actions, token, runtime)
		session.State = plugin.QRLoginStateFailed
		return session, nil
	}
	runtime.cookies = mergeDouyinCookies(runtime.cookies, cookies)
	if hasDouyinLoginCookieMap(runtime.cookies) {
		settled := settleDouyinLoginSession(ctx, runtime.session)
		runtime.cookies = mergeDouyinCookies(runtime.cookies, settled)
		result := provider.finish(ctx, actions, token, runtime, plugin.QRLoginStateSucceeded)
		return result, nil
	}

	observedState, redirectURL, pollErr := provider.pollObservedState(ctx, runtime, token)
	if pollErr != nil {
		if ctx.Err() != nil {
			return session, ctx.Err()
		}
		if strings.Contains(pollErr.Error(), "risk control") {
			runtime.blockCount++
			if runtime.blockCount >= 2 {
				provider.closeRuntime(ctx, actions, token, runtime)
				session.State = plugin.QRLoginStateFailed
				return session, nil
			}
			session.State = plugin.NormalizeQRLoginState(runtime.lastState)
			if session.State == "" {
				session.State = plugin.QRLoginStatePendingScan
			}
			return session, nil
		}
		return session, pollErr
	}
	if observedState == plugin.QRLoginStateSucceeded && redirectURL != "" {
		_ = followDouyinBrowserRedirect(ctx, runtime.session, redirectURL)
	}
	signals, signalErr := readDouyinPageSignals(ctx, runtime.session)
	if signalErr == nil && observedState == plugin.QRLoginStateSucceeded {
		if loginCookies, waitErr := waitDouyinLoginCookies(ctx, runtime.session, 5*time.Second); waitErr == nil {
			runtime.cookies = mergeDouyinCookies(runtime.cookies, loginCookies)
		}
		if hasDouyinLoginCookieMap(runtime.cookies) {
			settled := settleDouyinLoginSession(ctx, runtime.session)
			runtime.cookies = mergeDouyinCookies(runtime.cookies, settled)
			result := provider.finish(ctx, actions, token, runtime, plugin.QRLoginStateSucceeded)
			return result, nil
		}
		observedState = plugin.QRLoginStatePendingConfirm
	}
	if observedState == plugin.QRLoginStateExpired {
		provider.closeRuntime(ctx, actions, token, runtime)
		session.State = plugin.QRLoginStateExpired
		return session, nil
	}
	current := runtime.lastState
	if current == "" {
		current = plugin.QRLoginStatePendingScan
	}
	if observedState == plugin.QRLoginStatePendingScan && signalErr == nil && (hasDouyinLoginMarkerMap(runtime.cookies) || signals.HasUserLogin) {
		observedState = plugin.QRLoginStatePendingConfirm
	}
	next := advanceDouyinBrowserState(current, observedState)
	if signalErr == nil && signals.VerificationRequired && next != plugin.QRLoginStatePendingScan {
		next = plugin.QRLoginStateVerificationRequired
	}
	runtime.lastState = next
	session.State = next
	session.Cookies = cloneDouyinCookies(runtime.cookies)
	return session, nil
}

func (provider *accountQRProvider) pollObservedState(ctx context.Context, runtime *douyinBrowserRuntime, token string) (string, string, error) {
	if runtime.pollMode == douyinPollActiveToken {
		return pollDouyinBrowserFallback(ctx, runtime.session, token)
	}
	raw, sequence := runtime.capture.LatestWithSequence("poll")
	if sequence == runtime.pollSeq {
		return "", "", nil
	}
	runtime.pollSeq = sequence
	if len(raw) == 0 {
		return plugin.QRLoginStatePendingScan, "", nil
	}
	return parseDouyinBrowserPollResponse(raw)
}

func (provider *accountQRProvider) finish(ctx context.Context, actions plugin.SourceActions, token string, runtime *douyinBrowserRuntime, state string) plugin.QRLoginSession {
	cookieHeader := plugin.CookieHeader(runtime.cookies)
	session := plugin.QRLoginSession{
		Token:   token,
		State:   state,
		Cookie:  cookieHeader,
		Cookies: cloneDouyinCookies(runtime.cookies),
	}
	if state == plugin.QRLoginStateSucceeded {
		if profile, err := fetchDouyinBrowserProfile(ctx, runtime.session); err == nil {
			session.Account = profile
		}
	}
	provider.closeRuntime(ctx, actions, token, runtime)
	return session
}

func (provider *accountQRProvider) closeRuntime(ctx context.Context, actions plugin.SourceActions, token string, runtime *douyinBrowserRuntime) {
	provider.mu.Lock()
	delete(provider.sessions, token)
	provider.mu.Unlock()
	if runtime != nil && runtime.session != nil {
		runtime.session.Close(ctx, actions)
	}
}

func (provider *accountQRProvider) Close(ctx context.Context, actions plugin.SourceActions, session plugin.QRLoginSession) {
	token := strings.TrimSpace(session.Token)
	provider.mu.Lock()
	runtime := provider.sessions[token]
	delete(provider.sessions, token)
	provider.mu.Unlock()
	if runtime != nil && runtime.session != nil {
		runtime.session.Close(ctx, actions)
	}
}

// clearDouyinLoginCookies removes residual login-state cookies from the
// persistent profile while keeping device and security cookies.
func clearDouyinLoginCookies(ctx context.Context, session *plugin.BrowserSession) error {
	return session.RunTimeout(ctx, 8*time.Second, chromedp.ActionFunc(func(runCtx context.Context) error {
		for _, name := range []string{
			"sessionid", "sessionid_ss", "sid_guard", "sid_tt", "uid_tt", "uid_tt_ss",
			"sid_ucp_v1", "ssid_ucp_v1", "passport_csrf_token", "passport_csrf_token_default",
			"passport_auth_mix_state", "passport_assist_user", "passport_mfa_token",
			"d_ticket", "LOGIN_STATUS", "__ac_nonce",
		} {
			if err := network.DeleteCookies(name).WithDomain(".douyin.com").Do(runCtx); err != nil {
				return err
			}
		}
		return nil
	}))
}

func classifyDouyinQRNetwork(rawURL string) string {
	endpoint, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(endpoint.Scheme, "https") {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(endpoint.Hostname()))
	if host != "www.douyin.com" && host != douyinBrowserLoginHost && host != "sso.douyin.com" {
		return ""
	}
	switch endpoint.Path {
	case douyinBrowserQRCodePath:
		return "qrcode"
	case douyinBrowserQRConnectPath:
		return "poll"
	default:
		return ""
	}
}

func waitDouyinBrowserQRCode(ctx context.Context, session *plugin.BrowserSession, capture *plugin.BrowserResponseCapture, now time.Time) (plugin.QRLoginSession, string, error) {
	initialTimer := time.NewTimer(5 * time.Second)
	defer initialTimer.Stop()
	for {
		if raw := capture.Latest("qrcode"); len(raw) > 0 {
			result, err := parseDouyinBrowserQRCodeResponse(raw, now)
			return result, douyinPollCaptured, err
		}
		select {
		case <-ctx.Done():
			return plugin.QRLoginSession{}, "", ctx.Err()
		case <-capture.Updated():
		case <-initialTimer.C:
			goto fallback
		}
	}
fallback:
	for attempt := 0; attempt < 3; attempt++ {
		raw, err := callDouyinQRCodeAPI(ctx, session)
		if err == nil {
			result, parseErr := parseDouyinBrowserQRCodeResponse(raw, now)
			return result, douyinPollActiveToken, parseErr
		}
		select {
		case <-ctx.Done():
			return plugin.QRLoginSession{}, "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if raw := capture.Latest("qrcode"); len(raw) > 0 {
		result, err := parseDouyinBrowserQRCodeResponse(raw, now)
		return result, douyinPollCaptured, err
	}
	return plugin.QRLoginSession{}, "", fmt.Errorf("douyin browser could not obtain a QR code")
}

func callDouyinQRCodeAPI(ctx context.Context, session *plugin.BrowserSession) (json.RawMessage, error) {
	js := fmt.Sprintf(`(function(){
var u = %q + '?aid=6383&service=' + encodeURIComponent('https://www.douyin.com/') + '&need_logo=true&t=' + Date.now();
return fetch(u, {credentials: 'include'}).then(function(r){ return r.text(); }).catch(function(e){ return '{"error":"'+String(e && e.message || e)+'"}'; });
})()`, "https://"+douyinBrowserLoginHost+douyinBrowserQRCodePath)
	raw, err := session.EvaluateString(ctx, js, true, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var check struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &check); err == nil && check.Error != "" {
		return nil, fmt.Errorf("douyin qrcode api call failed")
	}
	if len(raw) > douyinBrowserResponseLimit {
		return nil, fmt.Errorf("douyin qrcode api response is too large")
	}
	return json.RawMessage(raw), nil
}

func pollDouyinBrowserFallback(ctx context.Context, session *plugin.BrowserSession, token string) (string, string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("douyin browser qrcode poll token is missing")
	}
	checkURL := "https://" + douyinBrowserLoginHost + douyinBrowserQRConnectPath + "?" + url.Values{
		"aid":     {"6383"},
		"service": {"https://www.douyin.com/"},
		"token":   {token},
		"t":       {fmt.Sprintf("%d", time.Now().UnixMilli())},
	}.Encode()
	encodedURL, _ := json.Marshal(checkURL)
	script := fmt.Sprintf(`(function(){
return fetch(%s, {credentials: 'include'}).then(function(r){ return r.text(); }).catch(function(e){ return '{"error":"'+String(e && e.message || e)+'"}'; });
})()`, encodedURL)
	raw, err := session.EvaluateString(ctx, script, true, 5*time.Second)
	if err != nil {
		return "", "", err
	}
	var check struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &check); err == nil && check.Error != "" {
		return "", "", fmt.Errorf("douyin browser qrcode poll request failed")
	}
	if len(raw) > douyinBrowserResponseLimit {
		return "", "", fmt.Errorf("douyin browser qrcode poll response is too large")
	}
	return parseDouyinBrowserPollResponse([]byte(raw))
}

func followDouyinBrowserRedirect(ctx context.Context, session *plugin.BrowserSession, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.User != nil || !strings.EqualFold(endpoint.Scheme, "https") ||
		!plugin.HostMatches(endpoint.Hostname(), "douyin.com", "amemv.com", "bytedance.com") {
		return fmt.Errorf("douyin browser qrcode redirect is invalid")
	}
	return session.Navigate(ctx, endpoint.String())
}

type douyinPageSignals struct {
	HasUserLogin         bool `json:"has_user_login"`
	VerificationRequired bool `json:"verification_required"`
}

func readDouyinPageSignals(ctx context.Context, session *plugin.BrowserSession) (douyinPageSignals, error) {
	const script = `(function(){
function visible(el){
  if (!el) return false;
  var style = window.getComputedStyle(el);
  if (!style || style.display === 'none' || style.visibility === 'hidden' || Number(style.opacity) === 0) return false;
  var rect = el.getBoundingClientRect();
  return rect.width > 0 && rect.height > 0;
}
var selectors = ['input[autocomplete="one-time-code"]','input[placeholder*="验证码"]','input[placeholder*="短信"]','iframe[src*="captcha"]','[class*="captcha"]','[id*="captcha"]','[class*="slider"]'];
var challenge = selectors.some(function(selector){ return Array.prototype.some.call(document.querySelectorAll(selector), visible); });
if (!challenge) {
  var text = document.body ? document.body.innerText || '' : '';
  challenge = ['短信验证','安全验证','拖动滑块','输入验证码','验证身份'].some(function(term){ return text.indexOf(term) !== -1; });
}
var login = false;
try { var marker = localStorage.getItem('HasUserLogin'); login = marker === '1' || marker === 'true'; } catch (e) {}
return {has_user_login: login, verification_required: challenge};
})()`
	var signals douyinPageSignals
	if err := session.Evaluate(ctx, script, &signals, 3*time.Second); err != nil {
		return douyinPageSignals{}, err
	}
	return signals, nil
}

func settleDouyinLoginSession(ctx context.Context, session *plugin.BrowserSession) map[string]string {
	if err := session.RunTimeout(ctx, douyinBrowserSettleTimeout,
		chromedp.Navigate(douyinBrowserHomeURL),
		chromedp.WaitReady("body"),
		chromedp.Sleep(douyinBrowserSettleDelay),
	); err != nil {
		return nil
	}
	settled, err := session.Cookies(ctx, []string{"https://www.douyin.com/"})
	if err != nil {
		return nil
	}
	return settled
}

func waitDouyinLoginCookies(ctx context.Context, session *plugin.BrowserSession, timeout time.Duration) (map[string]string, error) {
	deadline := time.Now().Add(timeout)
	var last map[string]string
	for {
		cookies, err := session.Cookies(ctx, []string{"https://www.douyin.com/", "https://login.douyin.com/"})
		if err != nil {
			return last, err
		}
		last = cookies
		if hasDouyinLoginCookieMap(cookies) || time.Now().After(deadline) {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func fetchDouyinBrowserProfile(ctx context.Context, session *plugin.BrowserSession) (plugin.AccountProfile, error) {
	raw, err := session.EvaluateString(ctx, `(function(){
return fetch('/aweme/v1/web/user/profile/self/?aid=6383&t=' + Date.now(), {credentials: 'include'}).then(function(r){ return r.text(); }).catch(function(e){ return '{"error":"'+e.message+'"}'; });
})()`, true, 10*time.Second)
	if err != nil {
		return plugin.AccountProfile{}, err
	}
	var check struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &check); err == nil && check.Error != "" {
		return plugin.AccountProfile{}, fmt.Errorf("douyin browser profile fetch failed")
	}
	var response struct {
		StatusCode int `json:"status_code"`
		User       struct {
			UID          string `json:"uid"`
			ShortID      string `json:"short_id"`
			UniqueID     string `json:"unique_id"`
			Nickname     string `json:"nickname"`
			AvatarMedium struct {
				URLList []string `json:"url_list"`
			} `json:"avatar_medium"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return plugin.AccountProfile{}, err
	}
	if response.StatusCode != 0 {
		return plugin.AccountProfile{}, fmt.Errorf("douyin browser profile api status %d", response.StatusCode)
	}
	profile := plugin.AccountProfile{
		UID:      plugin.FirstNonEmpty(response.User.UniqueID, response.User.ShortID, response.User.UID),
		Nickname: strings.TrimSpace(response.User.Nickname),
	}
	if len(response.User.AvatarMedium.URLList) > 0 {
		profile.AvatarURL = strings.TrimSpace(response.User.AvatarMedium.URLList[0])
	}
	return profile, nil
}

func advanceDouyinBrowserState(current, observed string) string {
	current = plugin.NormalizeQRLoginState(current)
	observed = plugin.NormalizeQRLoginState(observed)
	if plugin.IsQRLoginTerminalState(current) {
		return current
	}
	if plugin.IsQRLoginTerminalState(observed) {
		return observed
	}
	rank := func(state string) int {
		switch state {
		case plugin.QRLoginStateVerificationRequired:
			return 2
		case plugin.QRLoginStatePendingConfirm:
			return 1
		default:
			return 0
		}
	}
	if rank(observed) >= rank(current) && observed != "" {
		return observed
	}
	if current != "" {
		return current
	}
	return plugin.QRLoginStatePendingScan
}

func parseDouyinBrowserQRCodeResponse(body []byte, now time.Time) (plugin.QRLoginSession, error) {
	var response struct {
		Message string `json:"message"`
		Data    struct {
			ErrorCode      int    `json:"error_code"`
			Description    string `json:"description"`
			QRCodeIndexURL string `json:"qrcode_index_url"`
			Token          string `json:"token"`
			ExpireTime     int64  `json:"expire_time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return plugin.QRLoginSession{}, fmt.Errorf("douyin browser qrcode response: %w", err)
	}
	if response.Data.ErrorCode != 0 {
		return plugin.QRLoginSession{}, fmt.Errorf("douyin browser qrcode create failed: %s", plugin.FirstNonEmpty(response.Data.Description, response.Message, "invalid response"))
	}
	token := strings.TrimSpace(response.Data.Token)
	qrcodeURL := strings.TrimSpace(response.Data.QRCodeIndexURL)
	if token == "" || qrcodeURL == "" {
		return plugin.QRLoginSession{}, fmt.Errorf("douyin browser qrcode create missing token or qrcode url")
	}
	expiresAt := now.Add(3 * time.Minute)
	if response.Data.ExpireTime > 0 {
		remoteExpiresAt := time.Unix(response.Data.ExpireTime, 0).UTC()
		if remoteExpiresAt.After(now) {
			expiresAt = remoteExpiresAt
		}
	}
	return plugin.QRLoginSession{Token: token, QRCodeURL: qrcodeURL, ExpiresAt: expiresAt, State: plugin.QRLoginStatePendingScan}, nil
}

func parseDouyinBrowserPollResponse(body []byte) (string, string, error) {
	var response struct {
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Message     string `json:"message"`
		Data        struct {
			ErrorCode   int             `json:"error_code"`
			Description string          `json:"description"`
			Status      json.RawMessage `json:"status"`
			RedirectURL string          `json:"redirect_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", "", fmt.Errorf("douyin browser qrcode poll response: %w", err)
	}
	if response.ErrorCode != 0 || response.Data.ErrorCode != 0 {
		message := plugin.FirstNonEmpty(response.Description, response.Data.Description, response.Message, "invalid response")
		if isDouyinQRCodePollBlocked(message) {
			return "", "", fmt.Errorf("risk control")
		}
		return "", "", fmt.Errorf("douyin browser qrcode poll failed")
	}
	switch douyinRawStatus(response.Data.Status) {
	case "", "1", "new":
		return plugin.QRLoginStatePendingScan, "", nil
	case "2", "scan", "scanned":
		return plugin.QRLoginStatePendingConfirm, "", nil
	case "3", "confirm", "confirmed", "success", "succeeded":
		return plugin.QRLoginStateSucceeded, strings.TrimSpace(response.Data.RedirectURL), nil
	case "4", "5", "expire", "expired", "cancel", "canceled", "cancelled":
		return plugin.QRLoginStateExpired, "", nil
	default:
		return "", "", fmt.Errorf("douyin browser qrcode poll returned an unknown state")
	}
}

func douyinRawStatus(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(strings.ToLower(text))
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return fmt.Sprintf("%d", number)
	}
	return strings.Trim(strings.ToLower(value), `"`)
}

func isDouyinQRCodePollBlocked(message string) bool {
	message = strings.TrimSpace(message)
	return strings.Contains(message, "安全风险") || strings.Contains(message, "已阻止此次访问")
}

func cloneDouyinCookies(cookies map[string]string) map[string]string {
	return plugin.CloneStringMap(cookies)
}

func mergeDouyinCookies(base map[string]string, next map[string]string) map[string]string {
	merged := cloneDouyinCookies(base)
	for key, value := range next {
		merged[key] = value
	}
	return merged
}

func hasDouyinLoginCookieMap(cookies map[string]string) bool {
	for _, name := range []string{"sessionid", "sessionid_ss", "sid_guard"} {
		if strings.TrimSpace(cookies[name]) != "" {
			return true
		}
	}
	return false
}

func hasDouyinLoginMarkerMap(cookies map[string]string) bool {
	for name, value := range cookies {
		if strings.EqualFold(strings.TrimSpace(name), "LOGIN_STATUS") && strings.TrimSpace(value) == "1" {
			return true
		}
	}
	return false
}
