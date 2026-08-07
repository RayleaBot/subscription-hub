package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliFollowCheckInterval = 6 * time.Hour
	bilibiliCooldownBase        = 5 * time.Minute
	bilibiliCooldownMax         = time.Hour
)

type bilibiliSourceResult struct {
	Checked      int
	Updates      []map[string]any
	Errors       []string
	AccountCount int
	DynamicOK    bool
	LiveOK       bool
}

type bilibiliWatch struct {
	Dynamic bool
	Live    bool
}

type bilibiliSource struct {
	client *bilibiliClient
}

func newBilibiliSource(actions pluginActions) *bilibiliSource {
	return &bilibiliSource{client: newBilibiliClient(actions)}
}

func (source *bilibiliSource) poll(ctx context.Context, subscriptions []subscription) bilibiliSourceResult {
	watched := watchedBilibiliSubjects(subscriptions)
	result := bilibiliSourceResult{
		Checked: len(watched), Updates: []map[string]any{}, Errors: []string{},
		DynamicOK: true, LiveOK: true,
	}
	for _, item := range watched {
		if item.Dynamic {
			result.DynamicOK = false
		}
		if item.Live {
			result.LiveOK = false
		}
	}
	if len(watched) == 0 {
		return result
	}
	accounts, err := readBilibiliAccounts(ctx, source.client.actions)
	result.AccountCount = len(accounts)
	if err != nil {
		result.Errors = append(result.Errors, ensureSentence(err.Error()))
		return result
	}
	dynamicUIDs := make([]string, 0)
	liveUIDs := make([]string, 0)
	for uid, item := range watched {
		if item.Dynamic {
			dynamicUIDs = append(dynamicUIDs, uid)
		}
		if item.Live {
			liveUIDs = append(liveUIDs, uid)
		}
	}
	sort.Strings(dynamicUIDs)
	sort.Strings(liveUIDs)
	if len(dynamicUIDs) > 0 {
		updates, failures, ok := source.pollDynamics(ctx, dynamicUIDs, accounts)
		result.Updates = append(result.Updates, updates...)
		result.Errors = append(result.Errors, failures...)
		result.DynamicOK = ok
	}
	if len(liveUIDs) > 0 {
		updates, failures, ok := source.pollLive(ctx, liveUIDs, accounts)
		result.Updates = append(result.Updates, updates...)
		result.Errors = append(result.Errors, failures...)
		result.LiveOK = ok
	}
	result.Errors = dedupeStrings(result.Errors)
	return result
}

func watchedBilibiliSubjects(subscriptions []subscription) map[string]bilibiliWatch {
	result := map[string]bilibiliWatch{}
	for _, item := range subscriptions {
		if !item.Enabled || item.Platform != "bilibili" || strings.TrimSpace(item.UID) == "" {
			continue
		}
		current := result[item.UID]
		for _, service := range normalizeServices(item.Services, "bilibili") {
			if service == "all" || service == "live" {
				current.Live = true
			}
			if service == "all" || service != "live" {
				current.Dynamic = true
			}
		}
		result[item.UID] = current
	}
	return result
}

func (source *bilibiliSource) orderedAccounts(ctx context.Context, accounts []bilibiliAccount) []bilibiliAccount {
	if len(accounts) == 0 {
		return nil
	}
	result, _ := source.client.actions.KVGet(ctx, "source:bilibili:account_offset")
	value, _ := actionStoredValue(result)
	offset := int(intScalar(value))
	start := offset % len(accounts)
	ordered := append([]bilibiliAccount(nil), accounts[start:]...)
	return append(ordered, accounts[:start]...)
}

func (source *bilibiliSource) advanceAccount(ctx context.Context, accounts []bilibiliAccount, selected bilibiliAccount) {
	index := 0
	for candidate := range accounts {
		if accounts[candidate].key() == selected.key() {
			index = candidate
			break
		}
	}
	_, _ = source.client.actions.KVSet(ctx, "source:bilibili:account_offset", (index+1)%len(accounts))
}

func (source *bilibiliSource) pollDynamics(ctx context.Context, uids []string, accounts []bilibiliAccount) ([]map[string]any, []string, bool) {
	failures := make([]string, 0)
	skipped := make([]time.Duration, 0)
	for _, account := range source.orderedAccounts(ctx, accounts) {
		if delay := source.cooldownRemaining(ctx, "dynamic", account); delay > 0 {
			skipped = append(skipped, delay)
			continue
		}
		followFailures, err := source.ensureFollowing(ctx, account, uids)
		failures = append(failures, followFailures...)
		if err != nil {
			source.recordError(ctx, "dynamic", account, err, nil)
			var typed *bilibiliSourceError
			if errors.As(err, &typed) && typed.cooldown() {
				source.rememberCooldown(ctx, "dynamic", account, typed)
			}
			failures = append(failures, source.friendlyError("动态", err))
			continue
		}
		document, err := source.client.requestJSON(ctx, "GET", bilibiliDynamicFeedEndpoint(), account, true, false, "", true)
		if err != nil {
			source.recordError(ctx, "dynamic", account, err, nil)
			var typed *bilibiliSourceError
			if errors.As(err, &typed) && typed.cooldown() {
				source.rememberCooldown(ctx, "dynamic", account, typed)
			}
			failures = append(failures, source.friendlyError("动态", err))
			continue
		}
		watched := map[string]bool{}
		for _, uid := range uids {
			watched[uid] = true
		}
		updates := make([]map[string]any, 0)
		for _, update := range dynamicUpdates(document) {
			uid := stringScalar(nestedValue(update, "author", "uid"))
			if !watched[uid] {
				continue
			}
			update["uid"] = uid
			updates = append(updates, update)
		}
		source.clearCooldown(ctx, "dynamic", account)
		source.advanceAccount(ctx, accounts, account)
		return updates, dedupeStrings(failures), true
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
		failures = append(failures, fmt.Sprintf("Bilibili 动态检查因平台风控暂停，剩余约 %d 分钟。", minutes))
	}
	return nil, dedupeStrings(failures), false
}

func (source *bilibiliSource) pollLive(ctx context.Context, uids []string, accounts []bilibiliAccount) ([]map[string]any, []string, bool) {
	failures := make([]string, 0)
	skipped := make([]time.Duration, 0)
	for _, account := range source.orderedAccounts(ctx, accounts) {
		if delay := source.cooldownRemaining(ctx, "live", account); delay > 0 {
			skipped = append(skipped, delay)
			continue
		}
		document, err := source.client.requestJSON(ctx, "GET", bilibiliLiveStatusEndpoint(uids), account, false, true, "", false)
		if err != nil {
			source.recordError(ctx, "live", account, err, nil)
			var typed *bilibiliSourceError
			if errors.As(err, &typed) && typed.cooldown() {
				source.rememberCooldown(ctx, "live", account, typed)
			}
			failures = append(failures, source.friendlyError("直播", err))
			continue
		}
		updates := source.liveTransitions(ctx, document, uids)
		source.clearCooldown(ctx, "live", account)
		source.advanceAccount(ctx, accounts, account)
		return updates, nil, true
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
		failures = append(failures, fmt.Sprintf("Bilibili 直播检查因平台风控暂停，剩余约 %d 分钟。", minutes))
	}
	return nil, dedupeStrings(failures), false
}

func (source *bilibiliSource) ensureFollowing(ctx context.Context, account bilibiliAccount, uids []string) ([]string, error) {
	csrf := cookieField(account.Cookie, "bili_jct")
	if csrf == "" {
		return []string{"Bilibili 账号 CK 缺少 bili_jct，无法自动关注订阅账号。"}, nil
	}
	failures := make([]string, 0)
	for _, uid := range uids {
		if uid == account.ProfileUID {
			continue
		}
		key := "source:bilibili:follow:" + account.key() + ":" + uid
		stateResult, _ := source.client.actions.KVGet(ctx, key)
		stored, _ := actionStoredValue(stateResult)
		state := mapValue(stored)
		if checkedAt := intScalar(state["checked_at"]); checkedAt > 0 && source.client.now().Sub(time.Unix(checkedAt, 0)) < bilibiliFollowCheckInterval {
			continue
		}
		relationURL := bilibiliRelationURL + "?" + url.Values{"fid": []string{uid}}.Encode()
		document, err := source.client.requestJSON(ctx, "GET", relationURL, account, true, false, "", true)
		if err != nil {
			var typed *bilibiliSourceError
			if errors.As(err, &typed) && (typed.cooldown() || typed.Kind == "auth") {
				return failures, err
			}
			failures = append(failures, "Bilibili 订阅账号 "+uid+" 自动关注失败。")
			source.recordError(ctx, "auto_follow", account, err, map[string]any{"uid": uid})
			continue
		}
		following := intScalar(nestedValue(document, "data", "attribute")) > 0
		if !following {
			body := url.Values{"fid": []string{uid}, "act": []string{"1"}, "re_src": []string{"11"}, "csrf": []string{csrf}}.Encode()
			if _, err := source.client.requestJSON(ctx, "POST", bilibiliFollowURL, account, true, false, body, false); err != nil {
				var typed *bilibiliSourceError
				if errors.As(err, &typed) && (typed.cooldown() || typed.Kind == "auth") {
					return failures, err
				}
				failures = append(failures, "Bilibili 订阅账号 "+uid+" 自动关注失败。")
				source.recordError(ctx, "auto_follow", account, err, map[string]any{"uid": uid})
				continue
			}
			following = true
		}
		_, _ = source.client.actions.KVSet(ctx, key, map[string]any{"checked_at": source.client.now().Unix(), "following": following})
	}
	return failures, nil
}

func (source *bilibiliSource) liveTransitions(ctx context.Context, document map[string]any, uids []string) []map[string]any {
	updates := make([]map[string]any, 0)
	for _, uid := range uids {
		update := liveUpdate(document, uid)
		if update == nil {
			continue
		}
		status := intScalar(update["live_status"])
		session := firstText(update["pub_ts"], update["room_id"], "unknown")
		key := "source:bilibili:live:" + uid
		previousResult, _ := source.client.actions.KVGet(ctx, key)
		previousValue, previousExists := actionStoredValue(previousResult)
		previous := mapValue(previousValue)
		current := map[string]any{"status": status, "session": session, "room_id": stringScalar(update["room_id"])}
		_, _ = source.client.actions.KVSet(ctx, key, current)
		if !previousExists || previous == nil {
			continue
		}
		previousStatus := intScalar(previous["status"])
		previousSession := firstText(previous["session"], "unknown")
		if previousStatus == status && (status == 0 || previousSession == session) {
			continue
		}
		if status == 1 {
			update["id"] = "live-" + uid + "-started-" + session
			update["live_event"] = "started"
		} else {
			update["id"] = "live-" + uid + "-ended-" + previousSession
			update["live_event"] = "ended"
		}
		update["uid"] = uid
		updates = append(updates, update)
	}
	return updates
}

func (source *bilibiliSource) cooldownKey(scope string, account bilibiliAccount) string {
	return "source:bilibili:cooldown:" + scope + ":" + account.key()
}

func (source *bilibiliSource) cooldownRemaining(ctx context.Context, scope string, account bilibiliAccount) time.Duration {
	result, _ := source.client.actions.KVGet(ctx, source.cooldownKey(scope, account))
	stored, _ := actionStoredValue(result)
	until := time.Unix(intScalar(nestedValue(stored, "until")), 0)
	remaining := until.Sub(source.client.now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (source *bilibiliSource) rememberCooldown(ctx context.Context, scope string, account bilibiliAccount, err *bilibiliSourceError) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	stored, _ := actionStoredValue(result)
	attempts := int(intScalar(nestedValue(stored, "attempts"))) + 1
	delay := bilibiliCooldownBase
	for index := 1; index < attempts && delay < bilibiliCooldownMax; index++ {
		delay *= 2
	}
	if delay > bilibiliCooldownMax {
		delay = bilibiliCooldownMax
	}
	_, _ = source.client.actions.KVSet(ctx, key, map[string]any{
		"attempts": attempts, "until": source.client.now().Add(delay).Unix(), "kind": err.Kind, "code": err.Code,
	})
}

func (source *bilibiliSource) clearCooldown(ctx context.Context, scope string, account bilibiliAccount) {
	key := source.cooldownKey(scope, account)
	result, _ := source.client.actions.KVGet(ctx, key)
	if _, exists := actionStoredValue(result); exists {
		_, _ = source.client.actions.KVDelete(ctx, key)
	}
}

func (source *bilibiliSource) recordError(ctx context.Context, scope string, account bilibiliAccount, err error, fields map[string]any) {
	payload := map[string]any{"scope": scope, "account_id": account.key(), "error": diagnosticExcerpt(err.Error(), 600)}
	var typed *bilibiliSourceError
	if errors.As(err, &typed) {
		payload["kind"] = typed.Kind
		payload["code"] = typed.Code
		payload["http_status"] = typed.HTTPStatus
	}
	for key, value := range fields {
		payload[key] = value
	}
	_, _ = source.client.actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "Bilibili 订阅源检查失败", Fields: payload})
}

func (source *bilibiliSource) friendlyError(label string, err error) string {
	var typed *bilibiliSourceError
	if !errors.As(err, &typed) {
		return "Bilibili " + label + "检查失败。"
	}
	switch typed.Kind {
	case "risk_control":
		return "Bilibili " + label + "检查被风控拦截，已切换账号或进入退避。"
	case "rate_limit":
		return "Bilibili " + label + "检查触发频率限制，已切换账号或进入退避。"
	case "auth":
		return "Bilibili " + label + "检查失败：账号 CK 已失效，请重新扫码。"
	case "signature":
		return "Bilibili " + label + "检查失败：WBI 签名不可用。"
	default:
		return "Bilibili " + label + "检查失败。"
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func ensureSentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "。") || strings.HasSuffix(value, "！") || strings.HasSuffix(value, "？") || strings.HasSuffix(value, ".") {
		return value
	}
	return value + "。"
}
