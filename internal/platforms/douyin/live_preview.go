package douyin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

var douyinRSCPushPattern = regexp.MustCompile(`(?:self|window)\.__rsc_f\.push\(\s*`)

func fetchDouyinLivePreview(ctx context.Context, client *douyinClient, ref *douyinPreviewRef, body string) (map[string]any, error) {
	var fetchErr error
	if body == "" {
		body, _, fetchErr = client.requestShareHTML(ctx, ref.URL)
	}
	live := douyinLivePreviewFromPage(body, ref)
	if live == nil {
		if accounts, err := readDouyinAccounts(ctx, client.actions); err == nil {
			body, fetchErr = requestDouyinHTMLAcrossAccounts(ctx, client.actions, accounts, ref.URL, douyinWebReferer)
			live = douyinLivePreviewFromPage(body, ref)
		}
	}
	if live == nil {
		if fetchErr != nil {
			return nil, errors.New(friendlyDouyinSourceError("抖音直播预览失败", fetchErr))
		}
		return nil, errors.New("没有找到这场抖音直播的信息")
	}
	return normalizeDouyinLive(client.actions.TimeLocation(), live, ""), nil
}

func douyinLivePreviewFromPage(body string, ref *douyinPreviewRef) map[string]any {
	documents := append(douyinDocumentsFromHTML(body), douyinRSCDataFromHTML(body)...)
	for _, document := range documents {
		var found map[string]any
		walkDouyinValue(document, 0, func(object map[string]any) bool {
			if found != nil {
				return false
			}
			live := douyinPreviewLiveRoom(object)
			if live == nil {
				return true
			}
			if ref.Kind == "live_reflow" && plugin.StringScalar(live["room_id"]) != ref.ID {
				// 旧场次分享链接可能返回同一主播的新场次，需核对分享链接里的主播标识。
				expectedUID := douyinSecUIDFromInput(ref.URL)
				if expectedUID == "" || plugin.NestedValue(live, "user", "sec_uid") != expectedUID {
					return true
				}
			}
			if webRID := plugin.StringScalar(live["web_rid"]); ref.Kind == "live" && webRID != "" && webRID != ref.ID {
				return true
			}
			found = live
			return false
		})
		if found != nil {
			return found
		}
	}
	return douyinLiveFromPage(body)
}

// 直播分享页通过 React Flight 数据帧返回房间信息，文本帧按字节长度跳过。
// 只解码 JSON，不执行页面脚本；拼接传输片段以支持同一帧跨多个 script。
func douyinRSCDataFromHTML(body string) []any {
	var stream strings.Builder
	for _, match := range douyinRSCPushPattern.FindAllStringIndex(body, -1) {
		var chunk []any
		decoder := json.NewDecoder(strings.NewReader(body[match[1]:]))
		if decoder.Decode(&chunk) == nil && len(chunk) == 2 && plugin.IntScalar(chunk[0]) == 1 {
			if payload, ok := chunk[1].(string); ok {
				stream.WriteString(payload)
			}
		}
	}
	remaining := stream.String()
	documents := make([]any, 0)
	for remaining != "" {
		remaining = strings.TrimLeft(remaining, "\r\n")
		id, record, ok := strings.Cut(remaining, ":")
		if !ok {
			break
		}
		if _, err := strconv.ParseUint(id, 16, 64); err != nil {
			break
		}
		if strings.HasPrefix(record, "T") {
			length, text, ok := strings.Cut(record[1:], ",")
			size, err := strconv.ParseUint(length, 16, 64)
			if !ok || err != nil || size > uint64(len(text)) {
				break
			}
			remaining = text[size:]
			continue
		}
		line, rest, _ := strings.Cut(record, "\n")
		remaining = rest
		var document any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if decoder.Decode(&document) == nil {
			documents = append(documents, document)
		}
	}
	return documents
}

func douyinPreviewLiveRoom(room map[string]any) map[string]any {
	owner := plugin.MapValue(room["owner"])
	roomID := plugin.FirstText(room["id_str"], room["idStr"], room["room_id_str"], room["id"])
	status := plugin.IntScalar(room["status"])
	if owner == nil || roomID == "" || (status != 2 && status != 4) {
		return nil
	}
	webRID := plugin.FirstText(owner["web_rid"], owner["webRid"], room["web_rid"])
	liveURL := "https://webcast.amemv.com/douyin/webcast/reflow/" + url.PathEscape(roomID)
	if webRID != "" {
		liveURL = "https://live.douyin.com/" + url.PathEscape(webRID)
	}
	stream := plugin.MapValue(plugin.FirstNonNil(room["stream_url"], room["streamUrl"]))
	sdk := plugin.MapValue(plugin.FirstNonNil(stream["live_core_sdk_data"], stream["liveCoreSdkData"]))
	pull := plugin.MapValue(plugin.FirstNonNil(sdk["pull_data"], sdk["pullData"]))
	return map[string]any{
		"id": plugin.FirstText(webRID, roomID), "room_id": roomID, "web_rid": webRID,
		"title": room["title"], "cover": douyinURLFromImage(room["cover"]), "url": liveURL,
		"live_status": map[bool]int{true: 1, false: 0}[status == 2],
		"start_time":  plugin.FirstNonNil(room["start_time"], room["startTime"]),
		"user_count":  plugin.FirstNonNil(room["user_count"], room["userCount"]),
		"user": map[string]any{
			"sec_uid": plugin.FirstText(owner["sec_uid"], owner["secUid"]), "nickname": owner["nickname"],
			"unique_id": plugin.FirstText(owner["display_id"], owner["displayId"]),
			"avatar":    douyinURLFromImage(plugin.FirstNonNil(owner["avatar_large"], owner["avatarLarge"], owner["avatar_thumb"], owner["avatarThumb"])),
		},
		"room": map[string]any{"stream_url": map[string]any{
			"flv_pull_url":     plugin.FirstNonNil(stream["flv_pull_url"], stream["flvPullUrl"]),
			"hls_pull_url_map": plugin.FirstNonNil(stream["hls_pull_url_map"], stream["hlsPullUrlMap"]),
			"hls_pull_url":     plugin.FirstNonNil(stream["hls_pull_url"], stream["hlsPullUrl"]),
			"live_core_sdk_data": map[string]any{"pull_data": map[string]any{
				"stream_data": plugin.FirstNonNil(pull["stream_data"], pull["streamData"]),
			}},
		}},
	}
}
