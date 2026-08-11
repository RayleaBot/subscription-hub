package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	Sign      string `json:"sign,omitempty"`
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
	return readBilibiliUserWithActions(ctx, event.Actions(), uid)
}

func readBilibiliUserWithActions(ctx context.Context, actions pluginActions, uid string) (bilibiliUser, error) {
	accounts, err := readBilibiliAccounts(ctx, actions)
	if err != nil {
		return bilibiliUser{}, err
	}
	values := bilibiliDeviceQuery()
	values.Set("mid", uid)
	values.Set("platform", "web")
	values.Set("web_location", "1550101")
	endpoint := bilibiliUserInfoURL + "?" + values.Encode()
	document, err := requestBilibiliAcrossAccounts(ctx, actions, accounts, "GET", endpoint, true, false)
	if err != nil {
		return bilibiliUser{}, errors.New(friendlyBilibiliSourceError("Bilibili 用户信息读取失败", err))
	}
	data := mapValue(document["data"])
	name := cleanText(firstNonNil(data["name"], data["uname"]))
	resolvedUID := firstText(data["mid"], uid)
	if digits(resolvedUID) == "" || name == "" {
		return bilibiliUser{}, errors.New("没有找到这个 Bilibili 用户")
	}
	user := bilibiliUser{
		UID:       resolvedUID,
		Name:      name,
		AvatarURL: normalizeBilibiliURL(firstNonNil(data["face"], data["avatar"], data["upic"])),
		Sign:      cleanText(data["sign"]),
	}
	user.Fans = readBilibiliFans(ctx, actions, accounts[0], resolvedUID)
	return user, nil
}

// readBilibiliFans 读取 UP 主粉丝数，失败时静默返回 0（卡片不显示粉丝行）。
func readBilibiliFans(ctx context.Context, actions pluginActions, account bilibiliAccount, uid string) int {
	endpoint := bilibiliRelationStatURL + "?" + url.Values{"vmid": []string{uid}}.Encode()
	document, err := newBilibiliClient(actions).requestJSON(ctx, "GET", endpoint, account, false, false, "", false)
	if err != nil {
		return 0
	}
	return int(intScalar(nestedValue(document, "data", "follower")))
}

func searchBilibili(ctx context.Context, event *rayleabot.EventContext, query string) ([]bilibiliUser, error) {
	return searchBilibiliWithActions(ctx, event.Actions(), query)
}

func searchBilibiliWithActions(ctx context.Context, actions pluginActions, query string) ([]bilibiliUser, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("用法：/b站搜索up UP昵称关键词")
	}
	accounts, err := readBilibiliAccounts(ctx, actions)
	if err != nil {
		return nil, err
	}
	values := bilibiliDeviceQuery()
	values.Set("search_type", "bili_user")
	values.Set("order", "totalrank")
	values.Set("page", "1")
	values.Set("pagesize", "5")
	values.Set("keyword", query)
	values.Set("web_location", "1430654")
	endpoint := bilibiliUserSearchURL + "?" + values.Encode()
	document, err := requestBilibiliAcrossAccounts(ctx, actions, accounts, "GET", endpoint, true, false)
	if err != nil {
		return nil, errors.New(friendlyBilibiliSourceError("Bilibili UP 搜索失败", err))
	}
	results := sliceValue(nestedValue(document, "data", "result"))
	users := make([]bilibiliUser, 0, len(results))
	for _, raw := range results {
		item := mapValue(raw)
		uid := stringScalar(item["mid"])
		name := cleanText(htmlTag.ReplaceAllString(firstText(item["uname"], item["name"]), ""))
		if uid != "" && name != "" {
			users = append(users, bilibiliUser{
				UID:       uid,
				Name:      name,
				AvatarURL: normalizeBilibiliURL(firstNonNil(item["upic"], item["face"], item["avatar"])),
				Fans:      int(intScalar(item["fans"])),
				Sign:      cleanText(firstNonNil(item["usign"], item["sign"])),
			})
			if len(users) == 5 {
				break
			}
		}
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("没有搜索到 Bilibili 用户：%s", query)
	}
	return users, nil
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

func sortedServices(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
