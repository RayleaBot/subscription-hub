package douyin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const douyinCooldownBase = 5 * time.Minute

const douyinCooldownMax = time.Hour

const douyinZeroPostsCache = 15 * time.Minute

type douyinSourceResult struct {
	Checked      int
	Updates      []map[string]any
	Errors       []string
	AccountCount int
	FeedOK       bool
	Paused       bool
	PauseReasons []string
	FailureKinds []string
	ReadyUIDs    map[string]bool
	accounts     []douyinAccount
}

type douyinSource struct {
	client       *douyinClient
	pauseReasons []string
	failureKinds []string
}

func newDouyinSource(actions plugin.SourceActions) *douyinSource {
	return &douyinSource{client: newDouyinClient(actions)}
}

func (source *douyinSource) poll(ctx context.Context, subscriptions []plugin.Subscription) douyinSourceResult {
	return source.pollSince(ctx, subscriptions, time.Time{})
}

func (source *douyinSource) pollSince(ctx context.Context, subscriptions []plugin.Subscription, notBefore time.Time) douyinSourceResult {
	return source.PollSinceWithStateContext(ctx, ctx, subscriptions, notBefore)
}

func (source *douyinSource) PollSinceWithStateContext(ctx, stateCtx context.Context, subscriptions []plugin.Subscription, notBefore time.Time) douyinSourceResult {
	source.pauseReasons = nil
	source.failureKinds = nil
	uids := watchedDouyinUIDs(subscriptions)
	result := douyinSourceResult{
		Checked: len(uids), Updates: []map[string]any{}, Errors: []string{}, FeedOK: true,
		ReadyUIDs: map[string]bool{},
	}
	if len(uids) == 0 {
		return result
	}
	result.FeedOK = false
	accounts, err := readDouyinAccounts(ctx, source.client.actions)
	result.AccountCount = len(accounts)
	if err != nil {
		result.Errors = append(result.Errors, plugin.EnsureSentence(err.Error()))
		result.FailureKinds = []string{"account_read"}
		return result
	}
	result.accounts = append([]douyinAccount(nil), accounts...)
	result.FeedOK = true
	seenUpdates := map[string]bool{}
	for _, uid := range uids {
		if ctx.Err() != nil {
			result.FeedOK = false
			result.Errors = append(result.Errors, plugin.SubscriptionCheckIncompleteMessage)
			source.failureKinds = append(source.failureKinds, plugin.FirstText(douyinErrorLogFields(ctx.Err())["kind"], "request"))
			break
		}
		resolvedUID, resolveErr := source.resolveWatchUID(ctx, uid)
		if resolveErr != nil {
			result.Errors = append(result.Errors, friendlyDouyinSourceError("抖音用户解析失败", resolveErr))
			source.failureKinds = append(source.failureKinds, plugin.FirstText(douyinErrorLogFields(resolveErr)["kind"], "resolve"))
			continue
		}
		uidNotBefore := time.Time{}
		if douyinUIDNeedsHistory(ctx, source.client.actions, subscriptions, uid) {
			uidNotBefore = notBefore
		}
		updates, failures, ready, complete := source.pollUser(ctx, resolvedUID, accounts)
		if !ready {
			result.PauseReasons = append(result.PauseReasons, source.pauseReasons...)
		}
		result.ReadyUIDs[uid] = ready
		result.ReadyUIDs[resolvedUID] = ready
		if !complete {
			result.FeedOK = false
		}
		result.Errors = append(result.Errors, failures...)
		for _, update := range updates {
			if !uidNotBefore.IsZero() {
				if publishedAt := plugin.IntScalar(update["pub_ts"]); publishedAt > 0 && time.Unix(publishedAt, 0).Before(notBefore) && plugin.StringScalar(update["service"]) != "live" {
					continue
				}
			}
			key := plugin.FirstText(update["platform"], "douyin") + ":" + plugin.StringScalar(update["id"])
			if key == "douyin:" || seenUpdates[key] {
				continue
			}
			seenUpdates[key] = true
			result.Updates = append(result.Updates, update)
		}
	}
	result.Errors = plugin.DedupeStrings(result.Errors)
	result.FailureKinds = plugin.DedupeStrings(source.failureKinds)
	result.PauseReasons = plugin.DedupeStrings(result.PauseReasons)
	result.Paused = len(result.PauseReasons) > 0
	return result
}

func watchedDouyinUIDs(subscriptions []plugin.Subscription) []string {
	seen := map[string]bool{}
	uids := make([]string, 0)
	for _, item := range subscriptions {
		if !item.Enabled || item.Platform != "douyin" {
			continue
		}
		uid := strings.TrimSpace(item.UID)
		if uid == "" || seen[uid] {
			continue
		}
		seen[uid] = true
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids
}

// douyinUIDNeedsHistory 对齐微博：已建立基线的订阅只回看投递时效窗口内的作品，
// 未建立基线的订阅拉取最新一页，用于初始化而不推送旧内容。
func douyinUIDNeedsHistory(ctx context.Context, actions plugin.SourceActions, subscriptions []plugin.Subscription, uid string) bool {
	for _, item := range subscriptions {
		if item.Enabled && item.Platform == "douyin" && strings.TrimSpace(item.UID) == uid && douyinFeedInitialized(ctx, actions, item) {
			return true
		}
	}
	return false
}

func (source *douyinSource) resolveWatchUID(ctx context.Context, uid string) (string, error) {
	if looksLikeDouyinSecUID(uid) {
		return uid, nil
	}
	key := "source:douyin:resolved_uid:" + uid
	result, _ := source.client.actions.KVGet(ctx, key)
	if stored, ok := plugin.ActionStoredValue(result); ok {
		if resolved := strings.TrimSpace(plugin.StringScalar(stored)); looksLikeDouyinSecUID(resolved) {
			return resolved, nil
		}
		if object := plugin.MapValue(stored); object != nil {
			if resolved := strings.TrimSpace(plugin.StringScalar(object["uid"])); looksLikeDouyinSecUID(resolved) {
				return resolved, nil
			}
		}
	}
	users, err := resolveDouyinUsersWithActions(ctx, source.client.actions, uid)
	if err != nil {
		return "", err
	}
	matched := matchDouyinUserByQuery(users, uid)
	if matched == nil && len(users) == 1 {
		matched = &users[0]
	}
	if matched == nil || !looksLikeDouyinSecUID(matched.UID) {
		return "", errors.New("没有解析到抖音 sec_uid")
	}
	_, _ = source.client.actions.KVSet(ctx, key, map[string]any{"uid": matched.UID, "name": matched.Name})
	return matched.UID, nil
}

func (source *douyinSource) pollUser(ctx context.Context, secUID string, accounts []douyinAccount) ([]map[string]any, []string, bool, bool) {
	failures := make([]string, 0)
	for _, account := range accounts {
		if ctx.Err() != nil {
			failures = append(failures, plugin.SubscriptionCheckIncompleteMessage)
			return nil, plugin.DedupeStrings(failures), false, false
		}
		if delay := source.cooldownRemaining(ctx, "feed", account); delay > 0 {
			continue
		}
		updates, live, liveObserved, err := source.fetchUserUpdates(ctx, secUID, account)
		if err != nil {
			source.recordError(ctx, "feed", account, err, map[string]any{"uid": secUID})
			var typed *douyinSourceError
			if errors.As(err, &typed) && typed.cooldown() {
				source.rememberCooldown(ctx, "feed", account, typed)
			}
			failures = append(failures, source.friendlyError(err))
			continue
		}
		source.clearCooldown(ctx, "feed", account)
		if liveUpdate := source.liveTransition(ctx, secUID, live, liveObserved); liveUpdate != nil {
			updates = append(updates, liveUpdate)
		}
		return updates, nil, true, true
	}
	return nil, plugin.DedupeStrings(failures), false, false
}

// fetchUserUpdates 读取用户最新作品与直播状态。优先走作品列表接口，无结果时回退用户主页 HTML。
// live 是未归一化的直播对象；liveObserved=false 表示本次响应没有携带直播状态字段。
func (source *douyinSource) fetchUserUpdates(ctx context.Context, secUID string, account douyinAccount) ([]map[string]any, map[string]any, bool, error) {
	updates := make([]map[string]any, 0)
	document, jsonErr := source.client.requestJSON(ctx, douyinAwemePostAPIURL(secUID), account, douyinUserPageURL(secUID))
	if jsonErr == nil {
		for _, aweme := range douyinAwemesFromValue(document) {
			if update := normalizeDouyinAweme(aweme); update != nil {
				updates = append(updates, update)
			}
		}
		live, observed := douyinLiveObservation(document, secUID)
		if len(updates) > 0 || live != nil || observed {
			source.clearZeroPostsCache(ctx, secUID)
			return updates, live, observed, nil
		}
		// JSON 成功但零作品且无直播观测：负缓存有效期内跳过 HTML 回退，
		// 避免每个检查周期都重复拉一次用户主页。
		if source.zeroPostsCached(ctx, secUID) {
			return nil, nil, false, nil
		}
	}
	if jsonErr != nil && !douyinHTMLFallbackAllowed(jsonErr) {
		// 账号被风控/限流/会话拦截/鉴权拒绝时，无签名 HTML 请求同样会撞
		// 验证页，跳过回退避免无谓请求继续累积风控风险。
		return nil, nil, false, jsonErr
	}
	body, htmlErr := source.client.requestHTML(ctx, douyinUserPageURL(secUID), account, douyinWebReferer)
	if htmlErr != nil {
		if jsonErr != nil {
			return nil, nil, false, jsonErr
		}
		return nil, nil, false, htmlErr
	}
	for _, aweme := range douyinAwemesFromPage(body) {
		if update := normalizeDouyinAweme(aweme); update != nil {
			updates = append(updates, update)
		}
	}
	live := douyinLiveFromPage(body)
	if !douyinLiveBelongsToUID(live, secUID) {
		live = nil
	}
	if len(updates) == 0 && live == nil {
		// HTML 也确认零作品：记负缓存，避免下个周期重复拉主页。
		source.rememberZeroPostsCache(ctx, secUID)
	}
	// 用户主页对直播状态有最终解释权：成功解析即视为已观测。
	return updates, live, true, nil
}

func (source *douyinSource) zeroPostsKey(secUID string) string {
	return "source:douyin:zero_posts:" + strings.TrimSpace(secUID)
}

func (source *douyinSource) zeroPostsCached(ctx context.Context, secUID string) bool {
	result, _ := source.client.actions.KVGet(ctx, source.zeroPostsKey(secUID))
	stored, _ := plugin.ActionStoredValue(result)
	until := time.Unix(plugin.IntScalar(plugin.NestedValue(stored, "until")), 0)
	return until.After(source.client.now())
}

func (source *douyinSource) rememberZeroPostsCache(ctx context.Context, secUID string) {
	_, _ = source.client.actions.KVSet(ctx, source.zeroPostsKey(secUID), map[string]any{"until": source.client.now().Add(douyinZeroPostsCache).Unix()})
}

func (source *douyinSource) clearZeroPostsCache(ctx context.Context, secUID string) {
	result, _ := source.client.actions.KVGet(ctx, source.zeroPostsKey(secUID))
	if _, exists := plugin.ActionStoredValue(result); exists {
		_, _ = source.client.actions.KVDelete(ctx, source.zeroPostsKey(secUID))
	}
}

// liveTransition 对齐 B 站直播会话语义：首次观测只记录基线，不开播推送；
// 未开播→开播（或更换会话）推送一条 started 更新，同一会话重复观测不推送；
// 下播只记录状态，不推送下播卡片。
func (source *douyinSource) liveTransition(ctx context.Context, secUID string, live map[string]any, observed bool) map[string]any {
	key := douyinLiveStateKey(secUID)
	result, _ := source.client.actions.KVGet(ctx, key)
	stored, exists := plugin.ActionStoredValue(result)
	previous := plugin.MapValue(stored)
	previousOn := exists && previous != nil && plugin.BoolScalar(previous["on_air"])
	if live == nil {
		if observed && previousOn {
			_, _ = source.client.actions.KVSet(ctx, key, map[string]any{
				"on_air": false, "session": plugin.StringScalar(previous["session"]), "room_id": plugin.StringScalar(previous["room_id"]), "updated_at": source.client.now().Unix(),
			})
		}
		return nil
	}
	session := plugin.FirstText(live["session"], live["web_rid"], live["room_id"], live["id"], "unknown")
	_, _ = source.client.actions.KVSet(ctx, key, map[string]any{
		"on_air": true, "session": session, "room_id": plugin.FirstText(live["room_id"], live["web_rid"]), "updated_at": source.client.now().Unix(),
	})
	if !exists || previous == nil {
		return nil
	}
	if previousOn && plugin.StringScalar(previous["session"]) == session {
		return nil
	}
	update := normalizeDouyinLive(live, secUID)
	if update == nil {
		return nil
	}
	update["id"] = "live-" + secUID + "-started-" + session
	update["live_event"] = "started"
	update["live_status"] = 1
	update["status_label"] = "直播中"
	update["summary"] = "直播中\n开播时间：" + plugin.StringScalar(update["created_at"])
	return update
}

func douyinLiveStateKey(secUID string) string {
	return "source:douyin:live:" + strings.TrimSpace(secUID)
}

func (source *douyinSource) cooldownKey(scope string, account douyinAccount) string {
	return "source:douyin:cooldown:" + scope + ":" + account.key()
}

func (source *douyinSource) cooldownRemaining(ctx context.Context, scope string, account douyinAccount) time.Duration {
	result, _ := source.client.actions.KVGet(ctx, source.cooldownKey(scope, account))
	stored, _ := plugin.ActionStoredValue(result)
	until := time.Unix(plugin.IntScalar(plugin.NestedValue(stored, "until")), 0)
	remaining := until.Sub(source.client.now())
	if remaining < 0 {
		return 0
	}
	if remaining > 0 {
		reason := "上次请求未完成"
		switch plugin.StringScalar(plugin.NestedValue(stored, "kind")) {
		case "auth":
			reason = "认证请求被拒绝，请查看服务器凭据检查结果"
		case "session_blocked":
			reason = "平台接口拒绝当前会话，不能据此判断 CK 失效"
		case "risk_control":
			reason = "平台风控拦截"
		case "rate_limit":
			reason = "平台限流"
		}
		source.pauseReasons = append(source.pauseReasons, fmt.Sprintf("抖音检查已暂停：%s；预计 %s 后重试。", reason, until.Local().Format("15:04:05")))
	}
	return remaining
}

func (source *douyinSource) rememberCooldown(ctx context.Context, scope string, account douyinAccount, err *douyinSourceError) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	stored, _ := plugin.ActionStoredValue(result)
	attempts := int(plugin.IntScalar(plugin.NestedValue(stored, "attempts"))) + 1
	delay := douyinCooldownBase
	for index := 1; index < attempts && delay < douyinCooldownMax; index++ {
		delay *= 2
	}
	if delay > douyinCooldownMax {
		delay = douyinCooldownMax
	}
	_, _ = source.client.actions.KVSet(ctx, key, map[string]any{
		"attempts": attempts, "until": source.client.now().Add(delay).Unix(), "kind": err.Kind,
	})
}

func (source *douyinSource) clearCooldown(ctx context.Context, scope string, account douyinAccount) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	if _, exists := plugin.ActionStoredValue(result); exists {
		_, _ = source.client.actions.KVDelete(ctx, key)
	}
}

func (source *douyinSource) recordError(ctx context.Context, scope string, account douyinAccount, err error, fields map[string]any) {
	payload := map[string]any{"scope": scope, "account_id": account.key()}
	for key, value := range douyinErrorLogFields(err) {
		payload[key] = value
	}
	for key, value := range fields {
		payload[key] = value
	}
	source.failureKinds = append(source.failureKinds, scope+":"+plugin.FirstText(payload["error_code"], payload["kind"], "request"))
	message := fmt.Sprintf("抖音账号 %s 检查失败：%s", account.key(), strings.TrimSpace(source.friendlyError(err)))
	_, _ = source.client.actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "debug", Message: message, Fields: payload})
}

func (source *douyinSource) friendlyError(err error) string {
	return friendlyDouyinSourceError("抖音检查失败", err)
}

// logDouyinFailure 写结构化诊断日志（仅错误类型与状态码，不含响应正文，避免泄露 CK）。
func logDouyinFailure(ctx context.Context, actions plugin.SourceActions, message string, err error) {
	if actions == nil || err == nil {
		return
	}
	completeMessage := friendlyDouyinSourceError(message, err)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: completeMessage, Fields: douyinErrorLogFields(err)})
}
