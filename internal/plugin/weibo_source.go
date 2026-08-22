package plugin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	weiboCooldownBase = 5 * time.Minute
	weiboCooldownMax  = time.Hour
	weiboFeedMaxPages = 5
)

type weiboSourceResult struct {
	Checked      int
	Updates      []map[string]any
	Errors       []string
	AccountCount int
	FeedOK       bool
	ReadyUIDs    map[string]bool
	accounts     []weiboAccount
}

type weiboSource struct {
	client *weiboClient
}

func newWeiboSource(actions pluginActions) *weiboSource {
	return &weiboSource{client: newWeiboClient(actions)}
}

func (source *weiboSource) poll(ctx context.Context, subscriptions []subscription) weiboSourceResult {
	return source.pollSince(ctx, subscriptions, time.Time{})
}

func (source *weiboSource) pollSince(ctx context.Context, subscriptions []subscription, notBefore time.Time) weiboSourceResult {
	return source.pollSinceWithStateContext(ctx, ctx, subscriptions, notBefore)
}

func (source *weiboSource) pollSinceWithStateContext(ctx, stateCtx context.Context, subscriptions []subscription, notBefore time.Time) weiboSourceResult {
	uids := watchedWeiboUIDs(subscriptions)
	result := weiboSourceResult{
		Checked: len(uids), Updates: []map[string]any{}, Errors: []string{}, FeedOK: true,
		ReadyUIDs: map[string]bool{},
	}
	if len(uids) == 0 {
		return result
	}
	result.FeedOK = false
	accounts, err := readWeiboAccounts(ctx, source.client.actions)
	result.AccountCount = len(accounts)
	if err != nil {
		result.Errors = append(result.Errors, ensureSentence(err.Error()))
		return result
	}
	result.accounts = append([]weiboAccount(nil), accounts...)
	result.FeedOK = true
	seenUpdates := map[string]bool{}
	for _, uid := range uids {
		if ctx.Err() != nil {
			result.FeedOK = false
			result.Errors = append(result.Errors, subscriptionCheckIncompleteMessage)
			break
		}
		uidNotBefore := time.Time{}
		if weiboUIDNeedsHistory(ctx, source.client.actions, subscriptions, uid) {
			uidNotBefore = notBefore
		}
		updates, failures, ready, complete := source.pollUserFeed(ctx, stateCtx, uid, accounts, uidNotBefore)
		result.ReadyUIDs[uid] = ready
		if !complete {
			result.FeedOK = false
		}
		result.Errors = append(result.Errors, failures...)
		for _, update := range updates {
			key := updatePlatform(update) + ":" + stringScalar(update["id"])
			if key == ":" || seenUpdates[key] {
				continue
			}
			seenUpdates[key] = true
			result.Updates = append(result.Updates, update)
		}
	}
	result.Errors = dedupeStrings(result.Errors)
	return result
}

func weiboUIDNeedsHistory(ctx context.Context, actions pluginActions, subscriptions []subscription, uid string) bool {
	for _, item := range subscriptions {
		if item.Enabled && item.Platform == "weibo" && strings.TrimSpace(item.UID) == uid && weiboFeedInitialized(ctx, actions, item) {
			return true
		}
	}
	return false
}

func watchedWeiboUIDs(subscriptions []subscription) []string {
	seen := map[string]bool{}
	uids := make([]string, 0)
	for _, item := range subscriptions {
		if !item.Enabled || item.Platform != "weibo" {
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

func (source *weiboSource) orderedAccounts(ctx context.Context, accounts []weiboAccount) []weiboAccount {
	if len(accounts) == 0 {
		return nil
	}
	result, _ := source.client.actions.KVGet(ctx, "source:weibo:account_offset")
	value, _ := actionStoredValue(result)
	offset := int(intScalar(value))
	start := offset % len(accounts)
	ordered := append([]weiboAccount(nil), accounts[start:]...)
	return append(ordered, accounts[:start]...)
}

func (source *weiboSource) advanceAccount(ctx context.Context, accounts []weiboAccount, selected weiboAccount) {
	index := 0
	for candidate := range accounts {
		if accounts[candidate].key() == selected.key() {
			index = candidate
			break
		}
	}
	_, _ = source.client.actions.KVSet(ctx, "source:weibo:account_offset", (index+1)%len(accounts))
}

func (source *weiboSource) pollUserFeed(ctx, stateCtx context.Context, uid string, accounts []weiboAccount, notBefore time.Time) ([]map[string]any, []string, bool, bool) {
	failures := make([]string, 0)
	skipped := make([]time.Duration, 0)
	resumeCursor := ""
	if !notBefore.IsZero() {
		resumeCursor = source.feedResumeCursor(ctx, uid)
	}
	for _, account := range source.orderedAccounts(ctx, accounts) {
		if ctx.Err() != nil {
			failures = append(failures, subscriptionCheckIncompleteMessage)
			return nil, dedupeStrings(failures), false, false
		}
		if delay := source.cooldownRemaining(ctx, "feed", account); delay > 0 {
			skipped = append(skipped, delay)
			continue
		}
		updates := make([]map[string]any, 0)
		seenUpdates := map[string]bool{}
		seenCursors := map[string]bool{}
		cursor := ""
		complete := false
		failed := false
		ready := false
		freshPages := 0
		usingResume := false
		for page := 0; page < weiboFeedMaxPages; page++ {
			if ctx.Err() != nil {
				checkpointCursor := cursor
				if resumeCursor != "" && !usingResume {
					checkpointCursor = resumeCursor
				}
				source.rememberFeedResumeCursor(stateCtx, uid, checkpointCursor)
				failures = append(failures, subscriptionCheckIncompleteMessage)
				return updates, dedupeStrings(failures), ready, false
			}
			document, requestErr := source.client.requestJSON(ctx, weiboUserFeedURL(uid, cursor), account, weiboMobileReferer)
			if requestErr != nil {
				if ctx.Err() != nil {
					checkpointCursor := cursor
					if resumeCursor != "" && !usingResume {
						checkpointCursor = resumeCursor
					}
					source.rememberFeedResumeCursor(stateCtx, uid, checkpointCursor)
					failures = append(failures, subscriptionCheckIncompleteMessage)
					return updates, dedupeStrings(failures), ready, false
				}
				source.recordError(ctx, "feed", account, requestErr, map[string]any{"uid": uid, "page": page + 1})
				var typed *weiboSourceError
				if errors.As(requestErr, &typed) && typed.cooldown() {
					source.rememberCooldown(ctx, "feed", account, typed)
				}
				failures = append(failures, source.friendlyError(requestErr))
				failed = true
				break
			}
			ready = true
			if !usingResume {
				freshPages++
			}
			pageUpdates := weiboFeedUpdates(document)
			for _, update := range pageUpdates {
				key := updatePlatform(update) + ":" + stringScalar(update["id"])
				if stringScalar(update["uid"]) != uid || key == ":" || seenUpdates[key] {
					continue
				}
				seenUpdates[key] = true
				updates = append(updates, update)
			}
			if notBefore.IsZero() || weiboFeedReachedDateBoundary(pageUpdates, notBefore) {
				complete = true
				break
			}
			nextCursor := weiboFeedCursor(document)
			if resumeCursor != "" && !usingResume && (freshPages >= 2 || nextCursor == "" || nextCursor == "0") {
				if resumeCursor == nextCursor {
					usingResume = true
					resumeCursor = ""
				} else if resumeCursor != cursor && !seenCursors[resumeCursor] {
					seenCursors[resumeCursor] = true
					cursor = resumeCursor
					usingResume = true
					resumeCursor = ""
					continue
				}
			}
			if nextCursor == "" || nextCursor == "0" || nextCursor == cursor || seenCursors[nextCursor] {
				complete = true
				break
			}
			seenCursors[nextCursor] = true
			cursor = nextCursor
			if ctx.Err() != nil {
				checkpointCursor := cursor
				if resumeCursor != "" && !usingResume {
					checkpointCursor = resumeCursor
				}
				source.rememberFeedResumeCursor(stateCtx, uid, checkpointCursor)
				failures = append(failures, subscriptionCheckIncompleteMessage)
				return updates, dedupeStrings(failures), ready, false
			}
		}
		if failed {
			continue
		}
		if !complete {
			failures = append(failures, "微博账号 "+uid+" 更新较多，本轮分页预算已用完，将在后续检查继续读取。")
			source.rememberFeedResumeCursor(stateCtx, uid, cursor)
		} else {
			source.clearFeedResumeCursor(stateCtx, uid)
		}
		source.clearCooldown(ctx, "feed", account)
		source.advanceAccount(ctx, accounts, account)
		return updates, dedupeStrings(failures), ready, complete
	}
	if len(skipped) > 0 && len(failures) == 0 {
		minimum := skipped[0]
		for _, delay := range skipped[1:] {
			if delay < minimum {
				minimum = delay
			}
		}
		minutes := int((minimum + time.Minute - 1) / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		failures = append(failures, fmt.Sprintf("微博检查因 H5 会话阻断、平台风控或限流暂停，剩余约 %d 分钟。", minutes))
	}
	return nil, dedupeStrings(failures), false, false
}

func (source *weiboSource) feedResumeKey(uid string) string {
	return "source:weibo:feed_resume:" + strings.TrimSpace(uid)
}

func (source *weiboSource) feedResumeCursor(ctx context.Context, uid string) string {
	result, _ := source.client.actions.KVGet(ctx, source.feedResumeKey(uid))
	stored, _ := actionStoredValue(result)
	return strings.TrimSpace(stringScalar(nestedValue(stored, "cursor")))
}

func (source *weiboSource) rememberFeedResumeCursor(ctx context.Context, uid, cursor string) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" || cursor == "0" {
		return
	}
	_, _ = source.client.actions.KVSet(ctx, source.feedResumeKey(uid), map[string]any{
		"cursor": cursor, "updated_at": source.client.now().Unix(),
	})
}

func (source *weiboSource) clearFeedResumeCursor(ctx context.Context, uid string) {
	key := source.feedResumeKey(uid)
	result, _ := source.client.actions.KVGet(ctx, key)
	if _, exists := actionStoredValue(result); exists {
		_, _ = source.client.actions.KVDelete(ctx, key)
	}
}

func weiboFeedCursor(document map[string]any) string {
	return firstText(
		nestedValue(document, "data", "cardlistInfo", "since_id"),
		nestedValue(document, "cardlistInfo", "since_id"),
	)
}

func weiboFeedReachedDateBoundary(updates []map[string]any, notBefore time.Time) bool {
	if notBefore.IsZero() {
		return false
	}
	hasDatedUpdate := false
	hasFreshUpdate := false
	for _, update := range updates {
		if publishedAt := intScalar(update["pub_ts"]); publishedAt > 0 {
			hasDatedUpdate = true
			if !time.Unix(int64(publishedAt), 0).Before(notBefore) {
				hasFreshUpdate = true
			}
		}
	}
	return hasDatedUpdate && !hasFreshUpdate
}

func (source *weiboSource) cooldownKey(scope string, account weiboAccount) string {
	return "source:weibo:cooldown:" + scope + ":" + account.key()
}

func (source *weiboSource) cooldownRemaining(ctx context.Context, scope string, account weiboAccount) time.Duration {
	result, _ := source.client.actions.KVGet(ctx, source.cooldownKey(scope, account))
	stored, _ := actionStoredValue(result)
	until := time.Unix(intScalar(nestedValue(stored, "until")), 0)
	remaining := until.Sub(source.client.now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (source *weiboSource) rememberCooldown(ctx context.Context, scope string, account weiboAccount, err *weiboSourceError) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	stored, _ := actionStoredValue(result)
	attempts := int(intScalar(nestedValue(stored, "attempts"))) + 1
	delay := weiboCooldownBase
	for index := 1; index < attempts && delay < weiboCooldownMax; index++ {
		delay *= 2
	}
	if delay > weiboCooldownMax {
		delay = weiboCooldownMax
	}
	_, _ = source.client.actions.KVSet(ctx, key, map[string]any{
		"attempts": attempts, "until": source.client.now().Add(delay).Unix(), "kind": err.Kind,
	})
}

func (source *weiboSource) clearCooldown(ctx context.Context, scope string, account weiboAccount) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	if _, exists := actionStoredValue(result); exists {
		_, _ = source.client.actions.KVDelete(ctx, key)
	}
}

func (source *weiboSource) recordError(ctx context.Context, scope string, account weiboAccount, err error, fields map[string]any) {
	payload := map[string]any{"scope": scope, "account_id": account.key()}
	for key, value := range weiboErrorLogFields(err) {
		payload[key] = value
	}
	for key, value := range fields {
		payload[key] = value
	}
	_, _ = source.client.actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "微博订阅源检查失败", Fields: payload})
}

func (source *weiboSource) friendlyError(err error) string {
	var typed *weiboSourceError
	if !errors.As(err, &typed) {
		return "微博检查失败。"
	}
	switch typed.Kind {
	case "session_blocked":
		return "微博检查失败：H5 会话被拒绝（HTTP 432），可能是 CK 失效或平台风控；请在三方账号页检查 CK。"
	case "risk_control":
		return "微博检查被风控拦截，已切换账号或进入退避。"
	case "rate_limit":
		return "微博检查触发频率限制，已切换账号或进入退避。"
	case "auth":
		return "微博检查失败：账号 CK 已失效，请重新扫码。"
	default:
		if typed.HTTPStatus > 0 {
			return fmt.Sprintf("微博检查失败：上游请求异常（%s，HTTP %d）。", firstText(typed.Kind, "upstream"), typed.HTTPStatus)
		}
		return fmt.Sprintf("微博检查失败：上游请求异常（%s）。", firstText(typed.Kind, "upstream"))
	}
}
