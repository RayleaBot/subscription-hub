package plugin

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"
)

func RenderSubscriptionAuthor(item Subscription, value any) map[string]any {
	author := CloneJSONMap(MapValue(value))
	if author == nil {
		author = map[string]any{}
	}
	author["name"] = FirstText(author["name"], item.Name, item.UID)
	author["uid"] = FirstText(author["uid"], item.UID)
	avatar := StringScalar(author["avatar"])
	if !strings.HasPrefix(avatar, "https://") && !strings.HasPrefix(avatar, "http://") {
		// feed 里偶发只有相对 uri，不是可拉取地址；回退到订阅时校验过的存储头像。
		avatar = ""
	}
	author["avatar"] = FirstText(avatar, item.AvatarURL)
	return author
}

func BuildSubscriberCards(items []Subscriber) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		nickname := FirstText(item.Nickname, id)
		displayName := FirstText(item.GroupNickname, nickname)
		roleLabel := FirstText(item.RoleLabel, subscriberRoleLabel(item.Role))
		avatarURL := FirstText(item.AvatarURL, qqAvatarURL(id))
		result = append(result, map[string]any{
			"id": id, "nickname": nickname, "group_nickname": item.GroupNickname, "display_name": displayName,
			"title": item.Title, "role": item.Role, "role_label": roleLabel, "avatar_url": avatarURL, "uid_text": id,
		})
	}
	return result
}

func BuildContentMetric(key, label string, value any) map[string]any {
	if value == nil {
		return nil
	}
	rawText := strings.TrimSpace(StringScalar(value))
	if rawText == "" {
		return nil
	}
	count := IntScalar(value)
	if count < 0 {
		return nil
	}
	valueText := FormatCount(int(count))
	if count == 0 && rawText != "0" {
		valueText = rawText
	}
	return map[string]any{"key": key, "label": label, "value": count, "value_text": valueText, "icon_class": "metric-icon--" + key}
}

func SubscriberNames(cards []map[string]any) string {
	names := make([]string, 0, len(cards))
	for _, card := range cards {
		if name := FirstText(card["display_name"], card["nickname"], card["id"]); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

func subscriberRoleLabel(role string) string {
	switch role {
	case "super_admin":
		return "超级管理员"
	case "owner":
		return "群主"
	case "admin":
		return "管理员"
	case "member":
		return "群员"
	default:
		return ""
	}
}

func qqAvatarURL(id string) string {
	if Digits(id) == "" {
		return ""
	}
	return "https://q1.qlogo.cn/g?b=qq&nk=" + id + "&s=100"
}

func UIDText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "UID " + value
}

func ImageMaps(value any, limit int) []map[string]any {
	if typed, ok := value.([]map[string]any); ok {
		if len(typed) > limit {
			typed = typed[:limit]
		}
		result := make([]map[string]any, len(typed))
		copy(result, typed)
		return result
	}
	items := SliceValue(value)
	result := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if image := MapValue(raw); image != nil && StringScalar(image["url"]) != "" {
			result = append(result, image)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func LimitHTML(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return ""
	}
	if HTMLVisibleLength(value) <= limit {
		return value
	}
	var output strings.Builder
	openTags := make([]string, 0, 4)
	visible := 0
	truncated := false
	for offset := 0; offset < len(value); {
		if value[offset] == '<' {
			end := strings.IndexByte(value[offset:], '>')
			if end >= 0 {
				end += offset
				rawTag := value[offset : end+1]
				output.WriteString(rawTag)
				name, closing, selfClosing := htmlTagParts(rawTag)
				if name != "" && !htmlVoidTags[name] {
					if closing {
						for index := len(openTags) - 1; index >= 0; index-- {
							if openTags[index] == name {
								openTags = openTags[:index]
								break
							}
						}
					} else if !selfClosing {
						openTags = append(openTags, name)
					}
				}
				offset = end + 1
				continue
			}
		}
		if value[offset] == '&' {
			if end := strings.IndexByte(value[offset:], ';'); end > 0 && end <= 32 {
				end += offset
				entity := value[offset : end+1]
				decoded := html.UnescapeString(entity)
				length := utf8.RuneCountInString(decoded)
				if decoded != entity && visible+length <= limit {
					output.WriteString(entity)
					visible += length
					offset = end + 1
					continue
				}
			}
		}
		runeValue, size := utf8.DecodeRuneInString(value[offset:])
		if visible >= limit {
			truncated = true
			break
		}
		output.WriteRune(runeValue)
		visible++
		offset += size
	}
	if visible >= limit {
		truncated = true
	}
	if truncated {
		output.WriteString("...")
	}
	for index := len(openTags) - 1; index >= 0; index-- {
		output.WriteString("</" + openTags[index] + ">")
	}
	return output.String()
}

var htmlVoidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

func htmlTagParts(raw string) (name string, closing, selfClosing bool) {
	content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">"))
	if content == "" || strings.HasPrefix(content, "!") || strings.HasPrefix(content, "?") {
		return "", false, true
	}
	closing = strings.HasPrefix(content, "/")
	content = strings.TrimSpace(strings.TrimPrefix(content, "/"))
	selfClosing = strings.HasSuffix(content, "/")
	content = strings.TrimSpace(strings.TrimSuffix(content, "/"))
	if index := strings.IndexAny(content, " \t\r\n"); index >= 0 {
		content = content[:index]
	}
	return strings.ToLower(content), closing, selfClosing
}

func HTMLVisibleLength(value string) int {
	visible := 0
	for offset := 0; offset < len(value); {
		if value[offset] == '<' {
			if end := strings.IndexByte(value[offset:], '>'); end >= 0 {
				offset += end + 1
				continue
			}
		}
		if value[offset] == '&' {
			if end := strings.IndexByte(value[offset:], ';'); end > 0 && end <= 32 {
				entity := value[offset : offset+end+1]
				decoded := html.UnescapeString(entity)
				if decoded != entity {
					visible += utf8.RuneCountInString(decoded)
					offset += end + 1
					continue
				}
			}
		}
		_, size := utf8.DecodeRuneInString(value[offset:])
		visible++
		offset += size
	}
	return visible
}

func BuildMediaItems(images []map[string]any, duration, service string) []map[string]any {
	result := make([]map[string]any, 0, len(images))
	for index, image := range images {
		imageURL := StringScalar(image["url"])
		if imageURL == "" {
			continue
		}
		width := IntScalar(image["width"])
		height := IntScalar(image["height"])
		isGIF := strings.HasSuffix(strings.ToLower(strings.Split(imageURL, "?")[0]), ".gif")
		isLong := width > 0 && height > width*2
		labels := make([]string, 0, 2)
		classes := []string{"media-item"}
		if service == "视频" || service == "直播" || service == "专栏" {
			classes = append(classes, "media-item--wide")
		}
		if isGIF {
			labels = append(labels, "动图")
			classes = append(classes, "media-item--gif")
		}
		if isLong {
			labels = append(labels, "长图")
			classes = append(classes, "media-item--long")
		}
		itemDuration := StringScalar(image["duration_text"])
		if index == 0 && itemDuration == "" && service == "视频" {
			itemDuration = strings.TrimSpace(duration)
		}
		if itemDuration != "" {
			classes = append(classes, "media-item--video")
		}
		fallback := "assets/grid.svg"
		if service == "视频" || service == "直播" || service == "专栏" {
			fallback = "assets/cover.svg"
		}
		result = append(result, map[string]any{
			"url": imageURL, "class": strings.Join(classes, " "), "label": strings.Join(labels, " · "),
			"duration_text": itemDuration, "width": width, "height": height,
			"candidates": MediaItemCandidates(image["candidates"]),
			"fallback":   fallback,
		})
	}
	return result
}

// MediaItemCandidates 提取单条媒体数据的镜像候选地址（渲染资源取图重试用）。
func MediaItemCandidates(value any) []string {
	values := SliceValue(value)
	result := make([]string, 0, len(values))
	for _, raw := range values {
		if text := strings.TrimSpace(StringScalar(raw)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func MediaGridClass(count int) string {
	switch count {
	case 0:
		return ""
	case 1:
		return "media-grid--single"
	case 2, 4:
		return "media-grid--double"
	default:
		return "media-grid--triple"
	}
}

func FansText(fans int) string {
	if fans <= 0 {
		return ""
	}
	if fans >= 10000 {
		return "粉丝 " + strconv.FormatFloat(float64(fans)/10000, 'f', 1, 64) + "万"
	}
	return "粉丝 " + strconv.Itoa(fans)
}

func UserCardTargetText(action string, item Subscription) string {
	name := strings.TrimSpace(item.TargetName)
	if name == "" {
		if item.TargetType == "private" {
			name = "私聊"
		} else {
			name = "当前群聊"
		}
	}
	if action == "unsubscribed" {
		return "取消于：" + name
	}
	return "订阅到：" + name
}
