package plugin

import (
	"context"
	"errors"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (handler *Handler) Check(ctx context.Context, actions HostActions, current Settings) map[string]any {
	checkCtx, stateCtx, cancel := newSubscriptionCheckContexts(ctx)
	defer cancel()
	return handler.check(checkCtx, stateCtx, actions, current, handler.now())
}

func (handler *Handler) check(ctx, stateCtx context.Context, actions HostActions, current Settings, now time.Time) map[string]any {
	result := map[string]any{"handled": true, "checked": 0, "sent": 0, "errors": []string{}, "degraded": false}
	if !current.Enabled {
		result["skipped"] = "disabled"
		return result
	}
	groups := map[string][]Subscription{}
	for _, item := range current.Subscriptions {
		if item.Enabled && handler.byID[item.Platform].Baseline != nil {
			groups[item.Platform] = append(groups[item.Platform], item)
		}
	}
	if len(groups) == 0 {
		result["skipped"] = "no_checkable_subscriptions"
		return result
	}
	deliveryMaxAge := time.Duration(NormalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes)) * time.Minute
	source := map[string]any{}
	failures := []string{}
	sent, checked := 0, 0
	avatars := &AvatarCache{}
	identities := newSubscriberIdentityCache(ctx)
	type preparedUpdate struct {
		value Update
		err   error
	}
	prepared := map[string]preparedUpdate{}
	for _, platform := range handler.platforms {
		subscriptions := groups[platform.ID]
		if len(subscriptions) == 0 || ctx.Err() != nil {
			continue
		}
		session, ok := platform.NewSession(sourceBoundary(actions)).(CheckSession)
		if !ok {
			failures = append(failures, failureText(platform.ID, "source", "订阅源不支持检查。"))
			continue
		}
		polled := session.Poll(ctx, stateCtx, subscriptions, now.Add(-deliveryMaxAge))
		checked += polled.Checked
		source[platform.ID] = polled.Summary
		for _, failure := range polled.Errors {
			failures = append(failures, failureText(platform.ID, "source", failure))
		}
		for _, item := range subscriptions {
			if ctx.Err() != nil {
				break
			}
			needsBaseline := usesBaseline(platform, item)
			historyRecorded := true
			initialized, baselineAt := true, time.Time{}
			var stateErr error
			if needsBaseline {
				initialized, baselineAt, stateErr = ReadBaseline(ctx, actions, platform.Baseline.KeyPrefix+item.ID)
			}
			if stateErr != nil {
				failures = append(failures, failureText(platform.ID, "state", stateErr.Error()))
			}
			for _, update := range polled.Updates {
				if ctx.Err() != nil {
					break
				}
				service := StringScalar(update["service"])
				if item.Platform != StringScalar(update["platform"]) || item.UID != StringScalar(update["uid"]) || !platform.Services.Enabled(item, service) {
					continue
				}
				exempt := ContainsService(platform.Baseline.ExemptServices, service)
				if !exempt && stateErr != nil {
					continue
				}
				if !exempt && (!initialized || updateAtOrBeforeBaseline(update, baselineAt)) {
					if err := markUpdateSeen(stateCtx, actions, item, update, handler.now()); err != nil {
						historyRecorded = false
						failures = append(failures, failureText(platform.ID, "state", err.Error()))
					}
					continue
				}
				seen, err := updateSeen(ctx, actions, item, update)
				if err != nil {
					failures = append(failures, failureText(platform.ID, "state", err.Error()))
					continue
				}
				if seen {
					continue
				}
				if staleSubscriptionUpdate(update, now, deliveryMaxAge) {
					if err := markUpdateSeen(stateCtx, actions, item, update, handler.now()); err != nil {
						failures = append(failures, failureText(platform.ID, "state", err.Error()))
					}
					logStaleUpdate(ctx, actions, item, update, now)
					continue
				}
				key := strings.Join([]string{platform.ID, item.UID, service, StringScalar(update["id"])}, ":")
				content, exists := prepared[key]
				if !exists {
					content.value, content.err = session.Prepare(ctx, update)
					prepared[key] = content
				}
				if content.err != nil {
					failures = append(failures, failureText(platform.ID, "prepare", content.err.Error()))
				}
				if content.value == nil || ctx.Err() != nil {
					continue
				}
				identityFailures := []string{}
				item = refreshSubscribersForDelivery(ctx, actions, item, identities, &identityFailures)
				for _, failure := range identityFailures {
					failures = append(failures, failureText(platform.ID, "identity", failure))
				}
				card := session.UpdateCard(item, CloneJSONMap(content.value))
				if handler.deliverUpdate(ctx, stateCtx, actions, item, update, card, avatars, &failures) {
					sent++
				}
			}
			if polled.ReadyUIDs[strings.TrimSpace(item.UID)] && stateErr == nil && !initialized && needsBaseline {
				// Boolean baselines require every historical update to have a seen record.
				// Timestamped baselines can also filter history left after the work deadline.
				if !platform.Baseline.Timestamp && (ctx.Err() != nil || !historyRecorded) {
					continue
				}
				value := any(true)
				if platform.Baseline.Timestamp {
					value = map[string]any{"initialized": true, "baseline_at": now.Unix()}
				}
				if _, err := actions.KVSet(stateCtx, platform.Baseline.KeyPrefix+item.ID, value); err != nil {
					failures = append(failures, failureText(platform.ID, "state", err.Error()))
				}
			}
		}
	}
	if ctx.Err() == nil {
		trimSeenKeys(stateCtx, actions, current.Subscriptions)
	} else {
		failures = append(failures, failureText("subscription", "timeout", SubscriptionCheckIncompleteMessage))
	}
	result["checked"], result["sent"], result["source"] = checked, sent, source
	result["errors"], result["degraded"] = DedupeStrings(failures), len(failures) > 0
	if err := rememberSubscriptionCheck(stateCtx, actions, result, handler.now()); err != nil {
		failures = append(failures, failureText("subscription", "state", err.Error()))
		result["errors"], result["degraded"] = DedupeStrings(failures), true
	}
	return result
}

func usesBaseline(platform Platform, item Subscription) bool {
	for _, service := range platform.Services.NormalizeAll(item.Services) {
		if service == "all" || !ContainsService(platform.Baseline.ExemptServices, service) {
			return true
		}
	}
	return false
}

func (handler *Handler) deliverUpdate(ctx, stateCtx context.Context, actions HostActions, item Subscription, update Update, card CardRequest, avatars *AvatarCache, failures *[]string) bool {
	if ctx.Err() != nil {
		return false
	}
	fields := map[string]any{"platform": item.Platform, "stage": "render", "subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID}
	imagePath, err := handler.renderCard(ctx, actions, card, avatars, fields)
	if err != nil {
		*failures = append(*failures, failureText(item.Platform, "render", "订阅图片生成失败："+err.Error()))
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	_, err = actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: item.TargetType, TargetID: item.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	})
	if err != nil {
		*failures = append(*failures, failureText(item.Platform, "send", "订阅推送失败："+err.Error()))
		logSubscriptionFailure(stateCtx, actions, "订阅推送失败", item, err)
		return false
	}
	if err := markUpdateSeen(stateCtx, actions, item, update, handler.now()); err != nil {
		*failures = append(*failures, failureText(item.Platform, "state", err.Error()))
	}
	return true
}

func logStaleUpdate(ctx context.Context, actions SourceActions, item Subscription, update Update, now time.Time) {
	publishedAt := time.Unix(IntScalar(update["pub_ts"]), 0)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: "过期订阅更新已跳过", Fields: map[string]any{
		"platform": item.Platform, "subscription_id": item.ID, "update_id": StringScalar(update["id"]), "service": StringScalar(update["service"]),
		"published_at": publishedAt.UTC().Format(time.RFC3339), "age_seconds": int(now.Sub(publishedAt).Seconds()),
	}})
}

// ReadBaseline accepts both the legacy boolean and timestamped feed state.
func ReadBaseline(ctx context.Context, actions SourceActions, key string) (bool, time.Time, error) {
	result, err := actions.KVGet(ctx, key)
	if err != nil {
		return false, time.Time{}, err
	}
	value, exists := ActionStoredValue(result)
	if !exists {
		return false, time.Time{}, nil
	}
	state := MapValue(value)
	if state == nil {
		return BoolScalar(value), time.Time{}, nil
	}
	baselineAt := time.Time{}
	if timestamp := IntScalar(state["baseline_at"]); timestamp > 0 {
		baselineAt = time.Unix(timestamp, 0)
	}
	return BoolScalar(state["initialized"]), baselineAt, nil
}

var ErrUnsupportedPreview = errors.New("此平台不支持订阅预览。")
