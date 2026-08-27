package plugin

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

const douyinWalkMaxDepth = 8

var douyinRENDERDataPattern = regexp.MustCompile(`(?is)<script[^>]*id=["'](?:RENDER_DATA|ROUTER_DATA|__UNIVERSAL_DATA_FOR_REHYDRATION__)["'][^>]*>(.*?)</script>`)

func douyinUserFromValue(value any) douyinUser {
	user := douyinUserFromObject(mapValue(value))
	if user.UID != "" && user.Name != "" {
		return user
	}
	for _, key := range []string{"user", "user_info", "author", "author_info"} {
		if nested := douyinUserFromObject(mapValue(nestedValue(value, key))); nested.UID != "" && nested.Name != "" {
			return nested
		}
	}
	found := douyinUser{}
	walkDouyinValue(value, 0, func(object map[string]any) bool {
		candidate := douyinUserFromObject(object)
		if candidate.UID != "" && candidate.Name != "" {
			found = candidate
			return false
		}
		return true
	})
	return found
}

func collectDouyinUsers(value any, query string) []douyinUser {
	users := make([]douyinUser, 0, douyinSearchResultLimit)
	seen := map[string]bool{}
	walkDouyinValue(value, 0, func(object map[string]any) bool {
		if len(users) >= douyinSearchResultLimit {
			return false
		}
		user := douyinUserFromObject(object)
		if user.UID == "" || user.Name == "" || seen[user.UID] {
			return true
		}
		if query != "" && !douyinUserMatchesQuery(user, query) {
			if mapValue(object["user_info"]) == nil && mapValue(object["user"]) == nil {
				return true
			}
		}
		seen[user.UID] = true
		users = append(users, user)
		return true
	})
	return users
}

func douyinUserFromObject(object map[string]any) douyinUser {
	if object == nil {
		return douyinUser{}
	}
	if nested := mapValue(object["user_info"]); nested != nil {
		if user := douyinUserFromObject(nested); user.UID != "" {
			return user
		}
	}
	if nested := mapValue(object["user"]); nested != nil && object["sec_uid"] == nil && object["nickname"] == nil {
		if user := douyinUserFromObject(nested); user.UID != "" {
			return user
		}
	}
	uid := firstText(object["sec_uid"], object["sec_user_id"])
	if !looksLikeDouyinSecUID(uid) {
		return douyinUser{}
	}
	name := strings.TrimSpace(firstText(object["nickname"], object["nick_name"], object["name"]))
	if name == "" {
		return douyinUser{}
	}
	return douyinUser{
		UID:       uid,
		UniqueID:  firstText(object["unique_id"], object["short_id"], object["display_id"]),
		Name:      name,
		AvatarURL: douyinURLFromImage(firstNonNil(object["avatar_larger"], object["avatar_medium"], object["avatar_thumb"], object["avatar_url"], object["avatar"])),
		FansText:  douyinFansText(intScalar(firstNonNil(object["follower_count"], object["fans"], object["mplatform_followers_count"]))),
		Sign:      strings.TrimSpace(firstText(object["signature"], object["desc"], object["signature_display"])),
		Verify:    douyinVerifyText(object),
		VerifyOrg: douyinVerifyOrg(object),
	}
}

// 抖音个人认证用 custom_verify，机构/企业认证用 enterprise_verify_reason。
func douyinVerifyText(object map[string]any) string {
	return strings.TrimSpace(firstText(object["custom_verify"], object["enterprise_verify_reason"], nestedValue(object, "verification", "reason")))
}

func douyinVerifyOrg(object map[string]any) bool {
	return strings.TrimSpace(stringScalar(object["custom_verify"])) == "" && strings.TrimSpace(stringScalar(object["enterprise_verify_reason"])) != ""
}

func douyinUserMatchesQuery(user douyinUser, query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, "@")))
	if normalized == "" {
		return true
	}
	for _, value := range []string{user.UID, user.UniqueID, user.Name} {
		current := strings.ToLower(strings.TrimSpace(value))
		if current == "" {
			continue
		}
		if current == normalized || strings.Contains(current, normalized) || strings.Contains(normalized, current) {
			return true
		}
	}
	return false
}

func douyinUserFromPage(body string) douyinUser {
	for _, document := range douyinDocumentsFromHTML(body) {
		if user := douyinUserFromValue(document); user.UID != "" && user.Name != "" {
			return user
		}
	}
	return douyinUser{}
}

func douyinUsersFromSearchPage(body, query string) []douyinUser {
	users := make([]douyinUser, 0)
	seen := map[string]bool{}
	for _, document := range douyinDocumentsFromHTML(body) {
		for _, user := range collectDouyinUsers(document, query) {
			if seen[user.UID] {
				continue
			}
			seen[user.UID] = true
			users = append(users, user)
			if len(users) >= douyinSearchResultLimit {
				return users
			}
		}
	}
	return users
}

func douyinAwemesFromValue(value any) []map[string]any {
	awemes := make([]map[string]any, 0)
	seen := map[string]bool{}
	walkDouyinValue(value, 0, func(object map[string]any) bool {
		id := firstText(object["aweme_id"], object["awemeId"], object["group_id"])
		if id == "" || seen[id] {
			return true
		}
		if mapValue(object["author"]) == nil && firstText(object["desc"], object["description"]) == "" && mapValue(object["video"]) == nil && object["images"] == nil {
			return true
		}
		if object["aweme_id"] == nil && object["aweme_type"] == nil && object["video"] == nil && object["images"] == nil {
			return true
		}
		seen[id] = true
		awemes = append(awemes, object)
		return true
	})
	return awemes
}

func douyinAwemesFromPage(body string) []map[string]any {
	awemes := make([]map[string]any, 0)
	seen := map[string]bool{}
	for _, document := range douyinDocumentsFromHTML(body) {
		for _, aweme := range douyinAwemesFromValue(document) {
			id := firstText(aweme["aweme_id"], aweme["awemeId"], aweme["group_id"])
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			awemes = append(awemes, aweme)
		}
	}
	return awemes
}

func douyinLiveFromUserObject(object map[string]any) map[string]any {
	if object == nil {
		return nil
	}
	user := mapValue(object["user"])
	if user == nil {
		user = object
	}
	status := intScalar(firstNonNil(user["live_status"], nestedValue(user, "room_data", "status"), nestedValue(object, "room", "status")))
	roomID := firstText(user["room_id"], user["room_id_str"], nestedValue(user, "room", "room_id"), nestedValue(object, "room", "room_id_str"))
	webRID := firstText(user["web_rid"], nestedValue(user, "room", "owner", "web_rid"), nestedValue(object, "room", "web_rid"))
	if status != 1 || (roomID == "" && webRID == "") {
		return nil
	}
	title := firstText(nestedValue(user, "room", "title"), nestedValue(object, "room", "title"), "直播中")
	cover := douyinURLFromImage(firstNonNil(nestedValue(user, "room", "cover"), nestedValue(object, "room", "cover"), user["avatar_larger"], user["avatar_thumb"]))
	session := firstText(
		nestedValue(user, "room_data", "start_time"), nestedValue(user, "room", "start_time"),
		nestedValue(object, "room_data", "start_time"), nestedValue(object, "room", "start_time"),
		user["live_time"], user["live_start_time"],
	)
	liveID := firstText(webRID, roomID)
	if liveID == "" {
		return nil
	}
	liveURL := "https://www.douyin.com/user/" + url.PathEscape(firstText(user["sec_uid"], user["sec_user_id"]))
	if webRID != "" {
		liveURL = "https://live.douyin.com/" + url.PathEscape(webRID)
	}
	return map[string]any{
		"id": liveID, "room_id": roomID, "web_rid": webRID, "session": session,
		"title": title, "cover": cover, "url": liveURL, "user": user,
	}
}

// douyinLiveStatusObserved 判断对象是否显式携带直播状态字段；
// 字段缺失时直播状态未知，调用方不应据此改动已记录的会话状态。
func douyinLiveStatusObserved(object map[string]any) bool {
	if object == nil {
		return false
	}
	user := mapValue(object["user"])
	if user == nil {
		user = object
	}
	return user["live_status"] != nil || nestedValue(user, "room_data", "status") != nil || nestedValue(object, "room", "status") != nil
}

// douyinLiveObservation 从作品列表或资料响应中提取目标用户的直播状态。
// 只接受 sec_uid 匹配（或未标注）的直播对象，避免把页面推荐位算成订阅对象的状态。
// observed=false 表示响应没有携带任何直播状态字段。
func douyinLiveObservation(value any, secUID string) (map[string]any, bool) {
	secUID = strings.TrimSpace(secUID)
	if object := mapValue(value); object != nil {
		if live := douyinLiveFromUserObject(object); live != nil && douyinLiveBelongsToUID(live, secUID) {
			return live, true
		}
		if douyinLiveStatusObserved(object) {
			return nil, true
		}
	}
	var found map[string]any
	observed := false
	walkDouyinValue(value, 0, func(object map[string]any) bool {
		if found != nil {
			return false
		}
		if live := douyinLiveFromUserObject(object); live != nil && douyinLiveBelongsToUID(live, secUID) {
			found, observed = live, true
			return false
		}
		if !observed && douyinLiveStatusObserved(object) {
			if uid := firstText(object["sec_uid"], object["sec_user_id"], nestedValue(object, "user", "sec_uid")); uid == secUID {
				observed = true
			}
		}
		return true
	})
	return found, observed
}

func douyinLiveBelongsToUID(live map[string]any, secUID string) bool {
	if secUID == "" {
		return true
	}
	user := mapValue(live["user"])
	uid := firstText(user["sec_uid"], user["sec_user_id"])
	return uid == "" || uid == secUID
}

func douyinLiveFromPage(body string) map[string]any {
	for _, document := range douyinDocumentsFromHTML(body) {
		if live := douyinLiveFromUserObject(mapValue(document)); live != nil {
			return live
		}
		var found map[string]any
		walkDouyinValue(document, 0, func(object map[string]any) bool {
			if live := douyinLiveFromUserObject(object); live != nil {
				found = live
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func douyinDocumentsFromHTML(body string) []any {
	documents := make([]any, 0)
	for _, match := range douyinRENDERDataPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		decoded := strings.TrimSpace(html.UnescapeString(match[1]))
		if unescaped, err := url.QueryUnescape(decoded); err == nil {
			decoded = unescaped
		}
		var document any
		if err := json.Unmarshal([]byte(decoded), &document); err != nil {
			continue
		}
		documents = append(documents, document)
	}
	return documents
}

func walkDouyinValue(value any, depth int, visit func(map[string]any) bool) {
	if depth > douyinWalkMaxDepth || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if !visit(typed) {
			return
		}
		for _, child := range typed {
			walkDouyinValue(child, depth+1, visit)
		}
	case []any:
		for _, child := range typed {
			walkDouyinValue(child, depth+1, visit)
		}
	}
}

func douyinURLFromImage(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if strings.HasPrefix(text, "//") {
			return "https:" + text
		}
		return text
	case map[string]any:
		for _, item := range sliceValue(typed["url_list"]) {
			if text := douyinURLFromImage(item); text != "" {
				return text
			}
		}
		return firstText(typed["url"], typed["uri"], typed["avatar_url"])
	default:
		return strings.TrimSpace(stringScalar(value))
	}
}

func douyinFansText(count int64) string {
	if count <= 0 {
		return ""
	}
	if count >= 10000 {
		return "粉丝 " + formatCount(int(count))
	}
	return fmt.Sprintf("粉丝 %d", count)
}
