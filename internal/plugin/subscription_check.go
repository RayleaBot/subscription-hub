package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func checkSubscriptions(ctx context.Context, event *rayleabot.EventContext, current settings) map[string]any {
	return checkSubscriptionsWithActions(ctx, event.Actions(), current)
}

func checkSubscriptionsWithActions(ctx context.Context, actions pluginActions, current settings) map[string]any {
	checkCtx, stateCtx, cancel := newSubscriptionCheckContexts(ctx)
	defer cancel()
	return checkSubscriptionsWithActionsAtUsingStateContext(checkCtx, stateCtx, actions, current, time.Now())
}

func checkSubscriptionsWithActionsAt(ctx context.Context, actions pluginActions, current settings, now time.Time) map[string]any {
	return checkSubscriptionsWithActionsAtUsingStateContext(ctx, ctx, actions, current, now)
}

const (
	subscriptionCheckTimeout             = 45 * time.Second
	subscriptionCheckFinalizationReserve = 5 * time.Second
	subscriptionCheckIncompleteMessage   = "订阅检查未在本轮完成，剩余内容将在后续检查继续处理。"
)

func newSubscriptionCheckContexts(ctx context.Context) (context.Context, context.Context, context.CancelFunc) {
	now := time.Now()
	stateDeadline := now.Add(subscriptionCheckTimeout + subscriptionCheckFinalizationReserve)
	if parentDeadline, exists := ctx.Deadline(); exists && parentDeadline.Before(stateDeadline) {
		stateDeadline = parentDeadline
	}
	stateCtx, cancelState := context.WithDeadline(ctx, stateDeadline)
	checkDeadline := now.Add(subscriptionCheckTimeout)
	if reservedDeadline := stateDeadline.Add(-subscriptionCheckFinalizationReserve); reservedDeadline.Before(checkDeadline) {
		checkDeadline = reservedDeadline
	}
	checkCtx, cancelCheck := context.WithDeadline(stateCtx, checkDeadline)
	return checkCtx, stateCtx, func() {
		cancelCheck()
		cancelState()
	}
}

func checkSubscriptionsWithActionsAtUsingStateContext(ctx, stateCtx context.Context, actions pluginActions, current settings, now time.Time) map[string]any {
	deliveryMaxAge := time.Duration(normalizeDeliveryMaxAgeMinutes(current.DeliveryMaxAgeMinutes)) * time.Minute
	result := map[string]any{
		"handled": true, "checked": 0, "sent": 0, "errors": []string{}, "degraded": false,
	}
	if !current.Enabled {
		result["skipped"] = "disabled"
		return result
	}
	bilibiliSubs := make([]subscription, 0)
	weiboSubs := make([]subscription, 0)
	for _, item := range current.Subscriptions {
		if !item.Enabled {
			continue
		}
		switch item.Platform {
		case "bilibili":
			bilibiliSubs = append(bilibiliSubs, item)
		case "weibo":
			weiboSubs = append(weiboSubs, item)
		}
	}
	if len(bilibiliSubs) == 0 && len(weiboSubs) == 0 {
		result["skipped"] = "no_checkable_subscriptions"
		return result
	}

	source := map[string]any{}
	failures := make([]string, 0)
	sent := 0
	checked := 0
	preparedCache := map[string]map[string]any{}
	avatarCache := &bilibiliAvatarCache{}
	mediaCache := &weiboMediaCache{}
	identityCache := newSubscriberIdentityCache(ctx)

	if len(bilibiliSubs) > 0 {
		sourceResult := newBilibiliSource(actions).poll(ctx, bilibiliSubs)
		checked += sourceResult.Checked
		source["bilibili"] = map[string]any{
			"accounts": sourceResult.AccountCount, "dynamic_ok": sourceResult.DynamicOK, "live_ok": sourceResult.LiveOK,
		}
		failures = append(failures, sourceResult.Errors...)
		for _, item := range bilibiliSubs {
			if ctx.Err() != nil {
				break
			}
			dynamicInitialized := dynamicSourceInitialized(ctx, actions, item)
			for _, update := range sourceResult.Updates {
				if ctx.Err() != nil {
					break
				}
				if !subscriptionMatchesUpdate(item, update) {
					continue
				}
				if stringScalar(update["service"]) != "live" && !dynamicInitialized {
					markUpdateSeen(ctx, actions, item, update)
					continue
				}
				if updateSeen(ctx, actions, item, update) {
					continue
				}
				if staleSubscriptionUpdate(update, now, deliveryMaxAge) {
					markUpdateSeen(ctx, actions, item, update)
					logStaleBilibiliDynamic(ctx, actions, item, update, now)
					continue
				}
				prepared := prepareBilibiliUpdate(ctx, actions, update, preparedCache)
				if sendBilibiliUpdate(ctx, stateCtx, actions, item, update, prepared, avatarCache, identityCache, &failures) {
					sent++
				}
			}
			if ctx.Err() == nil && sourceResult.DynamicOK && subscriptionUsesDynamic(item) && !dynamicInitialized {
				_, _ = actions.KVSet(ctx, dynamicSourceKey(item), true)
			}
		}
	}

	if len(weiboSubs) > 0 && ctx.Err() == nil {
		weiboSource := newWeiboSource(actions)
		sourceResult := weiboSource.pollSinceWithStateContext(ctx, stateCtx, weiboSubs, now.Add(-deliveryMaxAge))
		longTextResolver := newWeiboLongTextResolver(actions, sourceResult.accounts)
		checked += sourceResult.Checked
		source["weibo"] = map[string]any{
			"accounts": sourceResult.AccountCount, "feed_ok": sourceResult.FeedOK,
		}
		failures = append(failures, sourceResult.Errors...)
		for _, item := range weiboSubs {
			if ctx.Err() != nil {
				break
			}
			initialized, baselineAt := weiboFeedState(ctx, actions, item)
			for _, update := range sourceResult.Updates {
				if ctx.Err() != nil {
					break
				}
				if !subscriptionMatchesUpdate(item, update) {
					continue
				}
				if !initialized {
					markUpdateSeen(ctx, actions, item, update)
					continue
				}
				if weiboUpdateAtOrBeforeBaseline(update, baselineAt) {
					markUpdateSeen(ctx, actions, item, update)
					continue
				}
				if updateSeen(ctx, actions, item, update) {
					continue
				}
				if staleSubscriptionUpdate(update, now, deliveryMaxAge) {
					markUpdateSeen(ctx, actions, item, update)
					logStaleWeiboMblog(ctx, actions, item, update, now)
					continue
				}
				prepared, incompleteText := longTextResolver.prepare(ctx, update)
				if incompleteText {
					failures = append(failures, weiboLongTextFailureText)
				}
				if sendWeiboUpdate(ctx, stateCtx, actions, item, update, prepared, avatarCache, mediaCache, identityCache, &failures) {
					sent++
				}
			}
			if sourceResult.ReadyUIDs[strings.TrimSpace(item.UID)] && !initialized {
				rememberWeiboFeedBaseline(stateCtx, actions, item, now)
			}
		}
	}

	if ctx.Err() != nil {
		failures = append(failures, subscriptionCheckIncompleteMessage)
	}
	failures = dedupeStrings(failures)
	result["checked"] = checked
	result["sent"] = sent
	result["errors"] = failures
	result["degraded"] = len(failures) > 0
	result["source"] = source
	rememberSubscriptionCheck(stateCtx, actions, result)
	return result
}

func staleSubscriptionUpdate(update map[string]any, now time.Time, deliveryMaxAge time.Duration) bool {
	if stringScalar(update["service"]) == "live" {
		return false
	}
	pubTS := intScalar(update["pub_ts"])
	if pubTS <= 0 {
		return false
	}
	publishedAt := time.Unix(int64(pubTS), 0)
	if publishedAt.After(now) {
		return false
	}
	return now.Sub(publishedAt) > deliveryMaxAge
}

func logStaleBilibiliDynamic(ctx context.Context, actions pluginActions, item subscription, update map[string]any, now time.Time) {
	publishedAt := time.Unix(int64(intScalar(update["pub_ts"])), 0)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   "info",
		Message: "Bilibili 过期动态已跳过",
		Fields: map[string]any{
			"subscription_id": item.ID,
			"update_id":       stringScalar(update["id"]),
			"service":         stringScalar(update["service"]),
			"published_at":    publishedAt.UTC().Format(time.RFC3339),
			"age_seconds":     int(now.Sub(publishedAt).Seconds()),
		},
	})
}

func logStaleWeiboMblog(ctx context.Context, actions pluginActions, item subscription, update map[string]any, now time.Time) {
	publishedAt := time.Unix(int64(intScalar(update["pub_ts"])), 0)
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   "info",
		Message: "微博过期博文已跳过",
		Fields: map[string]any{
			"subscription_id": item.ID,
			"update_id":       stringScalar(update["id"]),
			"service":         stringScalar(update["service"]),
			"published_at":    publishedAt.UTC().Format(time.RFC3339),
			"age_seconds":     int(now.Sub(publishedAt).Seconds()),
		},
	})
}

func updatePlatform(update map[string]any) string {
	if platform := strings.TrimSpace(stringScalar(update["platform"])); platform != "" {
		return platform
	}
	return "bilibili"
}

func subscriptionMatchesUpdate(item subscription, update map[string]any) bool {
	return item.Platform == updatePlatform(update) && item.UID == stringScalar(update["uid"]) && serviceEnabled(item, stringScalar(update["service"]))
}

func subscriptionUsesDynamic(item subscription) bool {
	for _, service := range normalizeServices(item.Services, "bilibili") {
		if service == "all" || service != "live" {
			return true
		}
	}
	return false
}

func dynamicSourceKey(item subscription) string {
	return "source:bilibili:dynamic:initialized:" + item.ID
}

func dynamicSourceInitialized(ctx context.Context, actions pluginActions, item subscription) bool {
	if !subscriptionUsesDynamic(item) {
		return true
	}
	result, _ := actions.KVGet(ctx, dynamicSourceKey(item))
	value, exists := actionStoredValue(result)
	return exists && boolScalar(value)
}

func weiboFeedSourceKey(item subscription) string {
	return "source:weibo:feed:initialized:" + item.ID
}

func weiboFeedInitialized(ctx context.Context, actions pluginActions, item subscription) bool {
	initialized, _ := weiboFeedState(ctx, actions, item)
	return initialized
}

func weiboFeedState(ctx context.Context, actions pluginActions, item subscription) (bool, time.Time) {
	result, _ := actions.KVGet(ctx, weiboFeedSourceKey(item))
	value, exists := actionStoredValue(result)
	if !exists {
		return false, time.Time{}
	}
	state := mapValue(value)
	if state == nil {
		return boolScalar(value), time.Time{}
	}
	baselineAt := time.Time{}
	if timestamp := intScalar(state["baseline_at"]); timestamp > 0 {
		baselineAt = time.Unix(timestamp, 0)
	}
	return boolScalar(state["initialized"]), baselineAt
}

func rememberWeiboFeedBaseline(ctx context.Context, actions pluginActions, item subscription, baselineAt time.Time) {
	_, _ = actions.KVSet(ctx, weiboFeedSourceKey(item), map[string]any{
		"initialized": true,
		"baseline_at": baselineAt.Unix(),
	})
}

func weiboUpdateAtOrBeforeBaseline(update map[string]any, baselineAt time.Time) bool {
	if baselineAt.IsZero() {
		return false
	}
	publishedAt := intScalar(update["pub_ts"])
	return publishedAt <= 0 || !time.Unix(publishedAt, 0).After(baselineAt)
}

func updateSeen(ctx context.Context, actions pluginActions, item subscription, update map[string]any) bool {
	result, _ := actions.KVGet(ctx, subscriptionUpdateKey(item, update))
	value, exists := actionStoredValue(result)
	return exists && boolScalar(value)
}

func markUpdateSeen(ctx context.Context, actions pluginActions, item subscription, update map[string]any) {
	_, _ = actions.KVSet(ctx, subscriptionUpdateKey(item, update), true)
}

func prepareBilibiliUpdate(ctx context.Context, actions pluginActions, update map[string]any, cache map[string]map[string]any) map[string]any {
	service := stringScalar(update["service"])
	updateID := stringScalar(update["id"])
	cacheKey := service + ":" + updateID
	if cached := cache[cacheKey]; cached != nil {
		return cloneJSONMap(cached)
	}
	prepared := cloneJSONMap(update)
	ref := parseBilibiliPreviewURL(stringScalar(prepared["url"]))
	needsImageTextDetail := service == "image_text" && stringScalar(prepared["summary"]) == "" && stringScalar(prepared["summary_html"]) == ""
	needsOriginal := service == "repost" && mapValue(prepared["original"]) == nil
	if ref != nil && (needsImageTextDetail || needsOriginal) && (ref.Kind == "opus" || ref.Kind == "dynamic") {
		if detailed, err := fetchBilibiliPreview(ctx, actions, ref); err == nil {
			if needsImageTextDetail {
				mergeMissingBilibiliDetail(prepared, detailed)
			}
			if needsOriginal {
				if original := mapValue(detailed["original"]); original != nil {
					prepared["original"] = original
				}
				for _, key := range []string{"summary", "summary_html"} {
					if stringScalar(prepared[key]) == "" && stringScalar(detailed[key]) != "" {
						prepared[key] = detailed[key]
					}
				}
			}
		}
	}
	cache[cacheKey] = cloneJSONMap(prepared)
	return prepared
}

func mergeMissingBilibiliDetail(target, source map[string]any) {
	for _, key := range []string{"summary", "summary_html"} {
		if stringScalar(target[key]) == "" && stringScalar(source[key]) != "" {
			target[key] = source[key]
		}
	}
	if mapValue(target["topic"]) == nil && mapValue(source["topic"]) != nil {
		target["topic"] = source["topic"]
	}
	if len(imageMaps(target["images"], 9)) == 0 && len(imageMaps(source["images"], 9)) > 0 {
		target["images"] = source["images"]
	}
}

func sendBilibiliUpdate(ctx, stateCtx context.Context, actions pluginActions, item subscription, update, prepared map[string]any, avatarCache *bilibiliAvatarCache, identityCache *subscriberIdentityCache, failures *[]string) bool {
	item = refreshSubscribersForDelivery(ctx, actions, item, identityCache, failures)
	data := buildBilibiliRenderData(item, prepared)
	inlineBilibiliUpdateAvatarsWithSharedCache(ctx, actions, data, avatarCache)
	imagePath, err := renderSubscriptionCardImage(ctx, actions, "bilibili-update", data, buildBilibiliFallback(data), map[string]any{
		"subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID,
	})
	if err != nil {
		*failures = append(*failures, "Bilibili 订阅图片生成失败。")
		return false
	}
	_, err = actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: item.TargetType, TargetID: item.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	})
	if err != nil {
		*failures = append(*failures, "Bilibili 订阅推送失败。")
		logSubscriptionFailure(ctx, actions, "Bilibili 订阅推送失败", item, err)
		return false
	}
	markUpdateSeen(stateCtx, actions, item, update)
	return true
}

func sendWeiboUpdate(ctx, stateCtx context.Context, actions pluginActions, item subscription, update, prepared map[string]any, avatarCache *bilibiliAvatarCache, mediaCache *weiboMediaCache, identityCache *subscriberIdentityCache, failures *[]string) bool {
	item = refreshSubscribersForDelivery(ctx, actions, item, identityCache, failures)
	data := buildWeiboRenderData(item, prepared)
	inlineBilibiliUpdateAvatarsWithSharedCache(ctx, actions, data, avatarCache)
	resolvedMedia, failedMedia := inlineWeiboUpdateMedia(ctx, actions, data, mediaCache)
	if failedMedia > 0 {
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
			Level:   "warn",
			Message: "微博订阅媒体读取不完整",
			Fields: map[string]any{
				"subscription_id": item.ID,
				"update_id":       stringScalar(update["id"]),
				"resolved":        resolvedMedia,
				"failed":          failedMedia,
			},
		})
		*failures = append(*failures, "部分微博媒体读取失败，卡片已使用占位图。")
	}
	imagePath, err := renderSubscriptionCardImage(ctx, actions, "weibo-update", data, buildWeiboFallback(data), map[string]any{
		"subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID,
	})
	if err != nil {
		*failures = append(*failures, "微博订阅图片生成失败。")
		return false
	}
	_, err = actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: item.TargetType, TargetID: item.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	})
	if err != nil {
		*failures = append(*failures, "微博订阅推送失败。")
		logSubscriptionFailure(ctx, actions, "微博订阅推送失败", item, err)
		return false
	}
	markUpdateSeen(stateCtx, actions, item, update)
	return true
}

func logSubscriptionFailure(ctx context.Context, actions pluginActions, message string, item subscription, err error) {
	fields := map[string]any{"subscription_id": item.ID, "target_type": item.TargetType, "target_id": item.TargetID}
	for key, value := range actionErrorLogFields(err) {
		fields[key] = value
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: message, Fields: fields})
}

func rememberSubscriptionCheck(ctx context.Context, actions pluginActions, result map[string]any) {
	errorsList := stringSlice(result["errors"])
	if len(errorsList) > 3 {
		errorsList = errorsList[:3]
	}
	_, _ = actions.KVSet(ctx, "source:subscription:last_result", map[string]any{
		"checked_at": time.Now().UTC().Format(time.RFC3339), "checked": intScalar(result["checked"]),
		"sent": intScalar(result["sent"]), "degraded": boolScalar(result["degraded"]),
		"errors": errorsList, "source": result["source"],
	})
}

func logSubscriptionCheck(ctx context.Context, actions pluginActions, result map[string]any) {
	failures := stringSlice(result["errors"])
	fields := map[string]any{"checked": intScalar(result["checked"]), "sent": intScalar(result["sent"])}
	if len(failures) > 0 {
		if len(failures) > 3 {
			failures = failures[:3]
		}
		fields["errors"] = failures
		_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "warn", Message: "订阅源检查降级", Fields: fields})
		return
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{Level: "info", Message: "订阅源检查完成", Fields: fields})
}

func subscriptionCheckSummary(result map[string]any) string {
	switch stringScalar(result["skipped"]) {
	case "disabled":
		return "订阅中心未启用。"
	case "no_checkable_subscriptions", "no_bilibili_subscriptions":
		return "没有可检查的订阅。"
	}
	line := fmt.Sprintf("订阅检查完成：检查 %d 个订阅账号，推送 %d 条更新。", intScalar(result["checked"]), intScalar(result["sent"]))
	failures := stringSlice(result["errors"])
	if len(failures) == 0 {
		return line
	}
	lines := []string{line}
	for index, failure := range failures {
		if index == 3 {
			break
		}
		lines = append(lines, "- "+failure)
	}
	return strings.Join(lines, "\n")
}

func stringSlice(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	result := make([]string, 0)
	for _, item := range sliceValue(value) {
		if text := stringScalar(item); text != "" {
			result = append(result, text)
		}
	}
	return result
}
