package douyin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const (
	douyinBrowserResolveTimeout  = 55 * time.Second
	douyinFetchAttemptTimeout    = 15 * time.Second
	douyinAcrawlerReadyTimeout   = 15 * time.Second
	douyinAcrawlerReadyPollEvery = 500 * time.Millisecond
	douyinBrowserSearchLimit     = 8
)

// searchDouyinViaBrowser resolves a nickname keyword inside the persistent
// login-profile browser. Douyin risk control ties the login session to device
// fingerprints, so in-browser requests can succeed where pure HTTP requests
// are blocked. Search needs no user interaction and runs headless.
func searchDouyinViaBrowser(ctx context.Context, actions plugin.SourceActions, query, cookie string) ([]douyinUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	session, err := plugin.LaunchBrowserSession(ctx, actions, rayleabot.BrowserLaunchRequest{
		Profile: "douyin-login",
		Mode:    plugin.ModeHeadless,
	})
	if err != nil {
		return nil, err
	}
	defer session.Close(ctx, actions)
	resolveCtx, cancel := context.WithTimeout(ctx, douyinBrowserResolveTimeout)
	defer cancel()
	if err := plugin.OverrideBrowserUserAgent(resolveCtx, session); err != nil {
		return nil, fmt.Errorf("douyin browser search: %w", err)
	}
	if strings.TrimSpace(cookie) != "" {
		if err := seedDouyinBrowserCookies(resolveCtx, session, cookie); err != nil {
			return nil, fmt.Errorf("douyin browser search: %w", err)
		}
	}
	searchPage := "https://www.douyin.com/search/" + url.QueryEscape(query) + "?type=user"
	if err := session.RunTimeout(resolveCtx, 15*time.Second,
		network.Enable(),
		chromedp.Navigate(searchPage),
		chromedp.WaitReady("body"),
	); err != nil {
		return nil, fmt.Errorf("douyin browser search: %w", err)
	}
	if err := waitDouyinAcrawler(resolveCtx, session); err != nil {
		return nil, fmt.Errorf("douyin browser search: %w", err)
	}
	body, err := fetchDouyinSearchDocument(resolveCtx, session, query)
	if err != nil {
		return nil, fmt.Errorf("douyin browser search: %w", err)
	}
	profiles, err := douyinSearchProfilesFromJSON(body, query)
	if err != nil {
		return nil, fmt.Errorf("douyin browser search: %w", err)
	}
	users := make([]douyinUser, 0, len(profiles))
	for _, profile := range profiles {
		users = append(users, douyinUser{
			UID:       profile.UID,
			UniqueID:  profile.UniqueID,
			Name:      profile.Nickname,
			AvatarURL: profile.AvatarURL,
		})
	}
	return users, nil
}

func seedDouyinBrowserCookies(ctx context.Context, session *plugin.BrowserSession, cookie string) error {
	return session.RunTimeout(ctx, 8*time.Second, chromedp.ActionFunc(func(runCtx context.Context) error {
		for name, value := range plugin.CookieMapFromHeader(cookie) {
			if err := network.SetCookie(name, value).WithDomain(".douyin.com").WithPath("/").Do(runCtx); err != nil {
				return err
			}
		}
		return nil
	}))
}

func waitDouyinAcrawler(ctx context.Context, session *plugin.BrowserSession) error {
	const probe = `(function(){
		if (window.byted_acrawler && typeof window.byted_acrawler.frontierSign === 'function') { return true; }
		return false;
	})()`
	deadline := time.Now().Add(douyinAcrawlerReadyTimeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("byted_acrawler did not become ready")
		}
		var ready bool
		if err := session.Evaluate(ctx, probe, &ready, 3*time.Second); err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(douyinAcrawlerReadyPollEvery):
		}
	}
}

func fetchDouyinSearchDocument(ctx context.Context, session *plugin.BrowserSession, query string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			searchPage := "https://www.douyin.com/search/" + url.QueryEscape(strings.TrimSpace(query)) + "?type=user"
			if err := session.RunTimeout(ctx, 15*time.Second,
				chromedp.Navigate(searchPage),
				chromedp.WaitReady("body"),
			); err != nil {
				return "", err
			}
			if err := waitDouyinAcrawler(ctx, session); err != nil {
				return "", err
			}
		}
		body, err := session.EvaluateString(ctx, douyinBrowserSearchScript(query), true, douyinFetchAttemptTimeout)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

func douyinBrowserSearchScript(query string) string {
	encoded, _ := json.Marshal(strings.TrimSpace(query))
	return fmt.Sprintf(`(async () => {
  const keyword = %s;
  const ua = navigator.userAgent || '';
  const chromeVersion = (ua.match(/Chrome\/([\d.]+)/) || [])[1] || '';
  const osName = ua.indexOf('Windows') >= 0 ? 'Windows' : (ua.indexOf('Mac OS') >= 0 ? 'Mac OS' : 'Linux');
  const browserPlatform = ua.indexOf('Windows') >= 0 ? 'Win32' : (ua.indexOf('Mac OS') >= 0 ? 'MacIntel' : 'Linux x86_64');
  const pcLibraDivert = ua.indexOf('Windows') >= 0 ? 'Windows' : (ua.indexOf('Mac OS') >= 0 ? 'Mac' : 'Linux');
  const params = new URLSearchParams({
    device_platform: 'webapp', aid: '6383', channel: 'channel_pc_web',
    search_channel: 'aweme_user_web', keyword: keyword, search_source: 'normal_search',
    query_correct_type: '1', is_filter_search: '0', from_group_id: '', disable_rs: '0',
    offset: '0', count: '12', need_filter_settings: '1', list_type: 'single',
    pc_search_top_1_params: JSON.stringify({enable_ai_search_top_1: 1}),
    update_version_code: '170400', pc_client_type: '1', pc_libra_divert: pcLibraDivert,
    support_h265: '1', support_dash: '1',
    cpu_core_num: String(navigator.hardwareConcurrency || 16),
    version_code: '170400', version_name: '17.4.0', cookie_enabled: 'true',
    screen_width: String(window.screen.width || 1920), screen_height: String(window.screen.height || 1080),
    browser_language: navigator.language || 'zh-CN', browser_platform: browserPlatform,
    browser_name: 'Chrome', browser_version: chromeVersion, browser_online: 'true',
    engine_name: 'Blink', engine_version: chromeVersion, os_name: osName, os_version: '10',
    device_memory: String(navigator.deviceMemory || 8), platform: 'PC', downlink: '10',
    effective_type: '4g', round_trip_time: '100'
  });
  const path = '/aweme/v1/web/discover/search/?' + params.toString();
  let signed = {};
  if (window.byted_acrawler && typeof window.byted_acrawler.frontierSign === 'function') {
    signed = window.byted_acrawler.frontierSign({url: path, method: 'GET'}) || {};
  }
  const xBogus = signed['X-Bogus'] || signed['x-bogus'] || '';
  const url = new URL(path, location.origin);
  if (xBogus) { url.searchParams.set('X-Bogus', xBogus); }
  return await new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('GET', url.toString(), true);
    xhr.withCredentials = true;
    if (xBogus) { xhr.setRequestHeader('X-Bogus', xBogus); }
    xhr.timeout = 13000;
    xhr.onload = () => resolve(xhr.responseText);
    xhr.onerror = () => reject(new Error('xhr network error'));
    xhr.ontimeout = () => reject(new Error('xhr timeout'));
    xhr.send();
  });
})()`, encoded)
}

func douyinSearchProfilesFromJSON(body string, query string) ([]plugin.AccountProfile, error) {
	text := strings.TrimSpace(body)
	if text == "" {
		return nil, nil
	}
	var check struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &check); err == nil && strings.TrimSpace(check.Error) != "" {
		return nil, fmt.Errorf("%s", check.Error)
	}
	var document any
	if err := json.Unmarshal([]byte(text), &document); err != nil {
		return nil, err
	}
	if object, ok := document.(map[string]any); ok {
		statusCode := plugin.JSONStringValue(object["status_code"])
		if statusCode != "" && statusCode != "0" {
			return nil, nil
		}
	}
	profiles := make([]plugin.AccountProfile, 0, douyinBrowserSearchLimit)
	seen := map[string]bool{}
	collectDouyinSearchProfiles(document, seen, &profiles, 0, false)
	return filterDouyinProfilesForQuery(profiles, query), nil
}

func filterDouyinProfilesForQuery(profiles []plugin.AccountProfile, query string) []plugin.AccountProfile {
	normalized := normalizedDouyinQuery(query)
	if normalized == "" {
		return profiles
	}
	filtered := make([]plugin.AccountProfile, 0, len(profiles))
	for _, profile := range profiles {
		if douyinProfileMatchesQuery(profile, normalized) {
			filtered = append(filtered, profile)
		}
	}
	return filtered
}

func normalizedDouyinQuery(query string) string {
	text := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if parsed, err := url.Parse(text); err == nil && parsed.Host != "" {
		parts := strings.FieldsFunc(strings.Trim(parsed.Path, "/"), func(r rune) bool { return r == '/' })
		if len(parts) > 0 {
			text = parts[len(parts)-1]
		}
	}
	return strings.ToLower(strings.TrimSpace(text))
}

func douyinProfileMatchesQuery(profile plugin.AccountProfile, query string) bool {
	for _, value := range []string{profile.UID, profile.UniqueID, profile.Nickname} {
		normalized := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
		if normalized == "" {
			continue
		}
		if normalized == query || strings.Contains(normalized, query) || strings.Contains(query, normalized) {
			return true
		}
	}
	return false
}

func collectDouyinSearchProfiles(value any, seen map[string]bool, profiles *[]plugin.AccountProfile, depth int, inSearchResult bool) {
	if depth > 8 || len(*profiles) >= douyinBrowserSearchLimit {
		return
	}
	switch item := value.(type) {
	case map[string]any:
		if data, ok := item["data"]; ok {
			collectDouyinSearchProfiles(data, seen, profiles, depth+1, true)
		}
		if userList, ok := item["user_list"].([]any); ok {
			for _, child := range userList {
				collectDouyinSearchProfiles(child, seen, profiles, depth+1, true)
				if len(*profiles) >= douyinBrowserSearchLimit {
					return
				}
			}
		}
		if inSearchResult {
			for _, key := range []string{"user_info", "user", "author", "author_user_info"} {
				if userInfo, ok := item[key].(map[string]any); ok {
					addDouyinProfile(userInfo, seen, profiles)
				}
			}
		}
		for _, child := range item {
			collectDouyinSearchProfiles(child, seen, profiles, depth+1, inSearchResult)
			if len(*profiles) >= douyinBrowserSearchLimit {
				return
			}
		}
	case []any:
		for _, child := range item {
			collectDouyinSearchProfiles(child, seen, profiles, depth+1, inSearchResult)
			if len(*profiles) >= douyinBrowserSearchLimit {
				return
			}
		}
	}
}

func addDouyinProfile(object map[string]any, seen map[string]bool, profiles *[]plugin.AccountProfile) {
	profile := plugin.AccountProfile{
		UID:      plugin.FirstNonEmpty(plugin.JSONStringValue(object["sec_uid"]), plugin.JSONStringValue(object["uid"])),
		UniqueID: plugin.FirstNonEmpty(plugin.JSONStringValue(object["unique_id"]), plugin.JSONStringValue(object["short_id"])),
		Nickname: plugin.JSONStringValue(object["nickname"]),
	}
	profile.AvatarURL = douyinAvatarURLFromObject(object)
	if strings.TrimSpace(profile.UID) == "" || strings.TrimSpace(profile.Nickname) == "" {
		return
	}
	key := strings.TrimSpace(profile.UID)
	if seen[key] {
		return
	}
	seen[key] = true
	*profiles = append(*profiles, profile)
}

func douyinAvatarURLFromObject(object map[string]any) string {
	for _, key := range []string{"avatar_medium", "avatar_thumb", "avatar_larger"} {
		if avatar, ok := object[key].(map[string]any); ok {
			if urlList, ok := avatar["url_list"].([]any); ok {
				for _, item := range urlList {
					if text := plugin.JSONStringValue(item); strings.TrimSpace(text) != "" {
						return text
					}
				}
			}
			if text := plugin.FirstNonEmpty(plugin.JSONStringValue(avatar["url"]), plugin.JSONStringValue(avatar["uri"])); text != "" {
				return text
			}
		}
	}
	return plugin.FirstNonEmpty(plugin.JSONStringValue(object["avatar_url"]), plugin.JSONStringValue(object["avatar"]))
}
