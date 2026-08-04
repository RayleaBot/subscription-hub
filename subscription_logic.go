package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type serviceCatalog struct {
	order   []string
	names   map[string]string
	aliases map[string]string
}

var (
	urlPattern = regexp.MustCompile(`https?://[^\s<>"，]+`)
	htmlTag    = regexp.MustCompile(`<[^>]+>`)
	services   = map[string]serviceCatalog{
		"bilibili": newServiceCatalog(
			[]string{"live", "video", "image_text", "article", "repost"},
			map[string]string{"all": "全部", "live": "直播", "video": "视频", "image_text": "图文", "article": "文章", "repost": "转发"},
			map[string]string{"全部": "all", "全量": "all", "所有": "all", "直播": "live", "视频": "video", "图文": "image_text", "动态": "image_text", "文章": "article", "专栏": "article", "转发": "repost"}),
		"weibo": newServiceCatalog(
			[]string{"post", "image", "video", "repost"},
			map[string]string{"all": "全部", "post": "微博", "image": "图片", "video": "视频", "repost": "转发"},
			map[string]string{"全部": "all", "全量": "all", "所有": "all", "微博": "post", "动态": "post", "文字": "post", "图片": "image", "图文": "image", "视频": "video", "转发": "repost"}),
		"douyin": newServiceCatalog(
			[]string{"video", "image_text", "live"},
			map[string]string{"all": "全部", "video": "视频", "image_text": "图文", "live": "直播"},
			map[string]string{"全部": "all", "全量": "all", "所有": "all", "视频": "video", "图文": "image_text", "图片": "image_text", "直播": "live"}),
		"netease_music": newServiceCatalog(
			[]string{"song", "album", "playlist", "artist"},
			map[string]string{"all": "全部", "song": "歌曲", "album": "专辑", "playlist": "歌单", "artist": "音乐人"},
			map[string]string{"全部": "all", "全量": "all", "所有": "all", "歌曲": "song", "音乐": "song", "单曲": "song", "专辑": "album", "歌单": "playlist", "音乐人": "artist", "歌手": "artist"}),
	}
)

func newServiceCatalog(order []string, names, aliases map[string]string) serviceCatalog {
	for key := range names {
		aliases[key] = key
	}
	return serviceCatalog{order: order, names: names, aliases: aliases}
}

func parseSubscriptionArgs(args []string, platform string) ([]string, string, bool) {
	values := make([]string, 0, len(args))
	for _, value := range args {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil, "", false
	}
	selected := []string{"all"}
	if service := normalizeService(values[0], platform); service != "" {
		selected = []string{service}
		values = values[1:]
	}
	query := strings.TrimSpace(strings.Join(values, " "))
	return normalizeServices(selected, platform), query, query != ""
}

func normalizeService(value, platform string) string {
	catalog, exists := services[platform]
	if !exists {
		return ""
	}
	value = strings.TrimSpace(value)
	if service, ok := catalog.aliases[value]; ok {
		return service
	}
	return catalog.aliases[strings.ToLower(value)]
}

func normalizeServices(values []string, platform string) []string {
	catalog, exists := services[platform]
	if !exists {
		return []string{"all"}
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		service := normalizeService(value, platform)
		if service == "" || seen[service] {
			continue
		}
		if service == "all" {
			return []string{"all"}
		}
		seen[service] = true
		result = append(result, service)
	}
	if len(result) == 0 || len(result) == len(catalog.order) {
		return []string{"all"}
	}
	return result
}

func mergeServices(existing, incoming []string, platform string) []string {
	if containsService(existing, "all") || containsService(incoming, "all") {
		return []string{"all"}
	}
	return normalizeServices(append(append([]string{}, existing...), incoming...), platform)
}

func removeServices(existing, removing []string, platform string) []string {
	catalog := services[platform]
	if containsService(removing, "all") {
		return nil
	}
	current := normalizeServices(existing, platform)
	if containsService(current, "all") {
		current = append([]string(nil), catalog.order...)
	}
	removeSet := map[string]bool{}
	for _, value := range removing {
		removeSet[value] = true
	}
	remaining := make([]string, 0, len(current))
	for _, value := range current {
		if !removeSet[value] {
			remaining = append(remaining, value)
		}
	}
	return remaining
}

func containsService(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func servicesText(values []string, platform string) string {
	catalog := services[platform]
	values = normalizeServices(values, platform)
	names := make([]string, 0, len(values))
	for _, value := range values {
		names = append(names, catalog.names[value])
	}
	return strings.Join(names, "、")
}

func serviceEnabled(item subscription, service string) bool {
	values := normalizeServices(item.Services, item.Platform)
	return containsService(values, "all") || containsService(values, service)
}

func subjectIDFromInput(platform, value string) string {
	value = strings.TrimSpace(value)
	for _, raw := range urlPattern.FindAllString(value, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		parts := pathParts(parsed.Path)
		switch platform {
		case "bilibili":
			if (host == "space.bilibili.com" || host == "m.bilibili.com") && len(parts) > 0 && digits(parts[0]) != "" {
				return parts[0]
			}
		case "weibo":
			if strings.HasSuffix(host, "weibo.com") || strings.HasSuffix(host, "weibo.cn") {
				if len(parts) > 1 && (parts[0] == "u" || parts[0] == "profile") {
					return safeSubjectID(parts[1])
				}
				if len(parts) > 0 {
					return safeSubjectID(parts[0])
				}
			}
		case "douyin":
			if strings.Contains(host, "douyin.com") || strings.HasSuffix(host, "iesdouyin.com") || strings.HasSuffix(host, "amemv.com") {
				for index, part := range parts {
					if (part == "user" || part == "video" || part == "note") && index+1 < len(parts) {
						return safeSubjectID(parts[index+1])
					}
				}
			}
		case "netease_music":
			if host == "music.163.com" {
				if id := parsed.Query().Get("id"); safeSubjectID(id) != "" {
					return safeSubjectID(id)
				}
				fragment, _ := url.Parse(strings.TrimPrefix(parsed.Fragment, "/"))
				if fragment != nil {
					return safeSubjectID(fragment.Query().Get("id"))
				}
			}
		}
	}
	if platform == "bilibili" {
		return digits(value)
	}
	return safeSubjectID(value)
}

func pathParts(value string) []string {
	items := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func safeSubjectID(value string) string {
	var result []rune
	for _, char := range strings.TrimSpace(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '.' || char == '-' {
			result = append(result, char)
		}
		if len(result) >= 96 {
			break
		}
	}
	return strings.Trim(string(result), "._-")
}

func digits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return ""
		}
	}
	return value
}

type bilibiliUser struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Fans      int    `json:"fans,omitempty"`
}

func resolveBilibiliUsers(ctx context.Context, event *rayleabot.EventContext, query string) ([]bilibiliUser, error) {
	query = strings.TrimSpace(query)
	if uid := subjectIDFromInput("bilibili", query); uid != "" {
		user, err := readBilibiliUser(ctx, event, uid)
		if err != nil {
			return nil, err
		}
		return []bilibiliUser{user}, nil
	}
	return searchBilibili(ctx, event, query)
}

func readBilibiliUser(ctx context.Context, event *rayleabot.EventContext, uid string) (bilibiliUser, error) {
	headers := bilibiliHeaders(readBilibiliCookie(ctx, event), uid)
	endpoint := "https://api.bilibili.com/x/space/acc/info?mid=" + url.QueryEscape(uid) + "&jsonp=jsonp"
	result, err := event.Actions().HTTPRequest(ctx, rayleabot.HTTPRequest{Method: "GET", URL: endpoint, Headers: headers, TimeoutSeconds: 12})
	if err != nil {
		return bilibiliUser{}, err
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			MID  any    `json:"mid"`
			Name string `json:"name"`
			Face string `json:"face"`
		} `json:"data"`
	}
	if err := decodeHTTPJSON(result, &response); err != nil {
		return bilibiliUser{}, err
	}
	if response.Code != 0 || strings.TrimSpace(response.Data.Name) == "" {
		return bilibiliUser{}, fmt.Errorf("Bilibili 用户信息读取失败：%s", response.Message)
	}
	return bilibiliUser{UID: scalarString(response.Data.MID, uid), Name: response.Data.Name, AvatarURL: response.Data.Face}, nil
}

func searchBilibili(ctx context.Context, event *rayleabot.EventContext, query string) ([]bilibiliUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("用法：/b站搜索up UP昵称关键词")
	}
	cookie := readBilibiliCookie(ctx, event)
	if cookie == "" {
		return nil, errors.New("没有可用的 Bilibili 账号 CK，请在 Web 三方账号页面保存账号")
	}
	endpoint := "https://api.bilibili.com/x/web-interface/search/type?search_type=bili_user&order=totalrank&page=1&pagesize=5&keyword=" + url.QueryEscape(query)
	result, err := event.Actions().HTTPRequest(ctx, rayleabot.HTTPRequest{Method: "GET", URL: endpoint, Headers: bilibiliHeaders(cookie, ""), TimeoutSeconds: 12})
	if err != nil {
		return nil, err
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Results []struct {
				MID   any    `json:"mid"`
				UName string `json:"uname"`
				UPic  string `json:"upic"`
				Fans  int    `json:"fans"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := decodeHTTPJSON(result, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("Bilibili UP 搜索失败：%s", response.Message)
	}
	users := make([]bilibiliUser, 0, len(response.Data.Results))
	for _, item := range response.Data.Results {
		uid := scalarString(item.MID, "")
		name := strings.TrimSpace(htmlTag.ReplaceAllString(item.UName, ""))
		if uid != "" && name != "" {
			users = append(users, bilibiliUser{UID: uid, Name: name, AvatarURL: normalizeURL(item.UPic), Fans: item.Fans})
		}
	}
	if len(users) == 0 {
		return nil, errors.New("没有找到匹配的 Bilibili UP 主")
	}
	return users, nil
}

func readBilibiliCookie(ctx context.Context, event *rayleabot.EventContext) string {
	result, err := event.Actions().ThirdPartyAccountRead(ctx, rayleabot.ThirdPartyAccountReadRequest{Platform: "bilibili"})
	if err != nil {
		return ""
	}
	var response struct {
		Accounts []struct {
			Cookie struct {
				Value string `json:"value"`
			} `json:"cookie"`
		} `json:"accounts"`
	}
	raw, _ := json.Marshal(result)
	_ = json.Unmarshal(raw, &response)
	for _, account := range response.Accounts {
		if cookie := strings.TrimSpace(account.Cookie.Value); cookie != "" {
			return cookie
		}
	}
	return ""
}

func bilibiliHeaders(cookie, uid string) map[string]string {
	headers := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124 Safari/537.36",
		"Referer":    "https://www.bilibili.com/",
	}
	if uid != "" {
		headers["Referer"] = "https://space.bilibili.com/" + uid + "/dynamic"
	}
	if cookie != "" {
		headers["Cookie"] = cookie
	}
	return headers
}

func decodeHTTPJSON(result map[string]any, target any) error {
	body, _ := result["body_text"].(string)
	if strings.TrimSpace(body) == "" {
		return errors.New("Bilibili 返回了空响应")
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		return errors.New("Bilibili 响应格式不正确")
	}
	return nil
}

func scalarString(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return strings.TrimSpace(typed)
		}
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	}
	return fallback
}

func normalizeURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}

func friendlyBilibiliError(err error) string {
	if err == nil {
		return "没有找到匹配的 Bilibili UP 主。"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "Bilibili 用户信息读取失败。"
	}
	if !strings.HasSuffix(message, "。") {
		message += "。"
	}
	return message
}

func searchBilibiliUsers(ctx context.Context, event *rayleabot.EventContext, query string) string {
	users, err := searchBilibili(ctx, event, query)
	if err != nil {
		return friendlyBilibiliError(err)
	}
	lines := []string{"Bilibili UP 搜索结果：" + strings.TrimSpace(query)}
	for index, user := range users {
		fans := ""
		if user.Fans > 0 {
			fans = "｜粉丝 " + formatCount(user.Fans)
		}
		lines = append(lines, fmt.Sprintf("%d. %s（UID %s）%s", index+1, user.Name, user.UID, fans))
	}
	return strings.Join(lines, "\n")
}

func formatCount(value int) string {
	if value < 10000 {
		return strconv.Itoa(value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", float64(value)/10000), "0"), ".") + "万"
}

func previewSubscriptionCard(ctx context.Context, event *rayleabot.EventContext, input string) error {
	input = strings.TrimSpace(input)
	service := normalizeService(input, "bilibili")
	previewURL := ""
	if matches := urlPattern.FindAllString(input, -1); len(matches) > 0 {
		previewURL = strings.TrimRight(matches[0], "。），,)")
		service = previewService(previewURL)
	}
	if service == "" || service == "all" {
		service = "video"
	}
	catalog := services["bilibili"]
	category := catalog.names[service]
	if category == "" {
		category = "动态"
	}
	if previewURL == "" {
		previewURL = "https://www.bilibili.com/"
	}
	data := map[string]any{
		"title": "订阅卡片预览", "headline": "Bilibili " + category + "预览", "platform": "bilibili",
		"service": service, "category": category, "summary": "这是订阅中心生成的预览内容，用于确认卡片排版与主题。",
		"content_text": "这是订阅中心生成的预览内容，用于确认卡片排版与主题。", "url": previewURL,
		"created_at": time.Now().Format("2006年01月02日 15:04"),
		"author":     map[string]any{"name": "RayleaBot", "uid": "preview"}, "author_uid_text": "UID preview",
		"images": []any{}, "image_count": 0, "media_items": []any{},
		"subscription": map[string]any{"platform": "bilibili", "uid": "preview", "name": "RayleaBot", "services": []string{service}},
		"subscribers":  []any{}, "subscriber_cards": []any{}, "subscriber_text": "",
	}
	fallback := fmt.Sprintf("订阅卡片预览\nBilibili %s\n%s", category, previewURL)
	result, err := event.Actions().RenderImage(ctx, rayleabot.RenderImageRequest{
		Template: "bilibili-update", Data: data, Output: "png", FallbackText: fallback,
	})
	if err == nil {
		if imagePath, _ := result["image_path"].(string); imagePath != "" {
			return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
		}
	}
	return event.SendText(fallback)
}

func previewService(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := pathParts(parsed.Path)
	if parsed.Hostname() == "live.bilibili.com" {
		return "live"
	}
	if len(parts) >= 2 && parts[0] == "video" {
		return "video"
	}
	if parsed.Hostname() == "t.bilibili.com" || (len(parts) >= 2 && parts[0] == "opus") {
		return "image_text"
	}
	return ""
}

func sortedServices(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
