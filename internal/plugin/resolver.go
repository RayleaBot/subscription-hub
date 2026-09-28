package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type ResolverMediaSource struct {
	Kind       string
	URLs       []string
	AudioURLs  []string
	Headers    map[string]string
	FileName   string
	Duration   int
	MergeAudio bool
}

const resolverFailureSendTimeout = 10 * time.Second

type ResolverMediaPlan struct {
	Sources []ResolverMediaSource
}

type ResolverMediaSkippedError struct {
	Reason string
}

func (err *ResolverMediaSkippedError) Error() string {
	return err.Reason
}

type ResolverMediaPlanningSession interface {
	ResolverMedia(context.Context, Update, ResolverMediaSettings) (ResolverMediaPlan, error)
}

type ResolverCardPreparingSession interface {
	PrepareResolverCard(context.Context, CardRequest) CardRequest
}

func (handler *Handler) handleResolverMessage(ctx context.Context, event *rayleabot.EventContext) error {
	if event.Event.EventType != "message.group" && event.Event.EventType != "message.private" {
		return event.Result(map[string]any{"handled": false})
	}
	current, err := handler.loadSettings(ctx, event)
	if err != nil {
		return err
	}
	target, enabled := resolverTargetForEvent(current.Resolver, event)
	if !enabled {
		return event.Result(map[string]any{"handled": false})
	}
	rawURL := firstResolverURL(event.Event.Message.PlainText)
	if rawURL == "" {
		return event.Result(map[string]any{"handled": false})
	}

	resolverCtx, cancel := context.WithTimeout(ctx, interactiveReplyTimeout)
	defer cancel()
	resolvedURL := expandResolverURL(resolverCtx, rawURL)
	platform := resolverPlatformForURL(resolvedURL)
	if platform == "" || !target.PlatformEnabled(platform) {
		return event.Result(map[string]any{"handled": false})
	}
	if handler.resolverCooldownHit(target, platform, canonicalResolverKey(resolvedURL), current.Resolver.Cooldowns) {
		return event.Result(map[string]any{"handled": true, "cooldown": true})
	}

	definition := handler.byID[platform]
	session, ok := definition.NewSession(sourceBoundary(handler.hostActions(event))).(PreviewSession)
	if !ok {
		return event.Result(map[string]any{"handled": false})
	}
	update, handled, err := session.Preview(resolverCtx, resolvedURL)
	if !handled {
		return event.Result(map[string]any{"handled": false})
	}
	if err != nil {
		return event.SendText(EnsureSentence(failureText(platform, "preview", err.Error())))
	}

	author := MapValue(update["author"])
	item := Subscription{
		ID:       "resolver-" + platform + "-" + event.Event.Target.ID,
		Platform: platform, UID: FirstText(author["uid"], update["uid"], "resolver"),
		Name:       FirstText(author["name"], resolverPlatformLabel(platform)),
		TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Services: []string{"all"}, Subscribers: []Subscriber{}, Enabled: true,
	}
	card := session.UpdateCard(item, update)
	if preparer, ok := session.(ResolverCardPreparingSession); ok {
		card = preparer.PrepareResolverCard(resolverCtx, card)
	}
	card.Template = resolverTemplateID(platform)
	imagePath, renderErr := handler.renderReplyCard(resolverCtx, handler.hostActions(event), card, map[string]any{"platform": platform, "stage": "resolve"})
	if renderErr != nil {
		if _, sendErr := handler.hostActions(event).MessageSend(resolverCtx, rayleabot.MessageSendRequest{
			TargetType: item.TargetType, TargetID: item.TargetID,
			Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(card.Fallback)}},
		}); sendErr != nil {
			return sendErr
		}
	} else if _, err := handler.hostActions(event).MessageSend(resolverCtx, rayleabot.MessageSendRequest{
		TargetType: item.TargetType, TargetID: item.TargetID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Image(imagePath)}},
	}); err != nil {
		return err
	}

	plan := genericResolverMedia(update)
	if planner, ok := session.(ResolverMediaPlanningSession); ok {
		planned, planErr := planner.ResolverMedia(resolverCtx, update, current.Resolver.Media)
		if planErr != nil {
			handler.sendResolverFailure(resolverCtx, event, resolverMediaPlanMessage(planErr))
			return event.Result(map[string]any{"handled": true, "card": true, "media": false})
		}
		if len(planned.Sources) > 0 {
			plan = planned
		}
	}
	if len(plan.Sources) > 0 {
		if handler.deferMediaForBackground(platform, plan) {
			if handler.enqueueDeferredMedia(event, platform, plan, current.Resolver.Media) {
				noticeCtx, noticeCancel := context.WithTimeout(resolverCtx, resolverFailureSendTimeout)
				defer noticeCancel()
				_, _ = handler.hostActions(event).MessageSend(noticeCtx, rayleabot.MessageSendRequest{
					TargetType: item.TargetType, TargetID: item.TargetID,
					Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text("视频正在后台下载，完成后会自动发送。")}},
				})
				return event.Result(map[string]any{"handled": true, "card": true, "media": "deferred"})
			}
			handler.sendResolverFailure(resolverCtx, event, "后台下载队列已满，请稍后重新分享链接")
			return event.Result(map[string]any{"handled": true, "card": true, "media": false})
		}
		release, gateErr := handler.mediaGate.acquire(resolverCtx, current.Resolver.Media.MediaConcurrency)
		if gateErr != nil {
			return gateErr
		}
		mediaErr := handler.deliverResolverMedia(resolverCtx, event, platform, update, plan, current.Resolver.Media)
		release()
		if mediaErr != nil {
			if mediaOutcomeUncertain(mediaErr) {
				handler.sendResolverFailure(resolverCtx, event, "媒体发送结果未确认，可能仍会送达；文件保留 24 小时，未自动重发")
			} else {
				handler.sendResolverFailure(resolverCtx, event, "媒体发送失败："+mediaErr.Error())
			}
			return event.Result(map[string]any{"handled": true, "card": true, "media": false})
		}
	}
	return event.Result(map[string]any{"handled": true, "card": true, "media": len(plan.Sources) > 0})
}

// resolverTargetForEvent returns the resolver switches that apply to the
// current message. Super admins bypass the per-target switches when the
// whitelist is enabled; cooldowns still key on the real target.
func resolverTargetForEvent(settings ResolverSettings, event *rayleabot.EventContext) (ResolverTarget, bool) {
	target, exists := settings.Target(event.Event.Target.Type, event.Event.Target.ID)
	if settings.SuperAdminWhitelist && ActorIsSuperAdmin(event) {
		target.Bilibili, target.Weibo, target.Douyin = true, true, true
		return target, target.TargetID != ""
	}
	return target, exists && (target.Bilibili || target.Weibo || target.Douyin)
}

func resolverTemplateID(platform string) string {
	switch platform {
	case "bilibili", "weibo", "douyin":
		return platform + "-resolver"
	default:
		return platform + "-update"
	}
}

func resolverMediaPlanMessage(err error) string {
	var skipped *ResolverMediaSkippedError
	if errors.As(err, &skipped) {
		return skipped.Error()
	}
	return "媒体解析失败：" + err.Error()
}

func firstResolverURL(text string) string {
	return strings.TrimRight(URLPattern.FindString(strings.TrimSpace(text)), "。；;、,.!?！？)]}》」'")
}

func resolverPlatformForURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "b23.tv", host == "bili2233.cn", host == "bilibili.com", strings.HasSuffix(host, ".bilibili.com"):
		return "bilibili"
	case host == "t.cn", host == "weibo.cn", host == "weibo.com", strings.HasSuffix(host, ".weibo.cn"), strings.HasSuffix(host, ".weibo.com"):
		return "weibo"
	case host == "douyin.com", host == "iesdouyin.com", host == "amemv.com", strings.HasSuffix(host, ".douyin.com"), strings.HasSuffix(host, ".iesdouyin.com"), strings.HasSuffix(host, ".amemv.com"):
		return "douyin"
	default:
		return ""
	}
}

func expandResolverURL(ctx context.Context, raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "b23.tv" && host != "bili2233.cn" && host != "t.cn" {
		return raw
	}
	client := &http.Client{Timeout: 8 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return raw
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 RayleaBot Link Resolver")
	response, err := client.Do(request)
	if err != nil {
		return raw
	}
	_ = response.Body.Close()
	if response.Request != nil && response.Request.URL != nil {
		return response.Request.URL.String()
	}
	return raw
}

func canonicalResolverKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	parsed.Fragment = ""
	query := url.Values{}
	for _, key := range []string{"modal_id", "id", "mid", "aweme_id"} {
		if value := strings.TrimSpace(parsed.Query().Get(key)); value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = path.Clean(parsed.Path)
	return parsed.String()
}

func (handler *Handler) resolverCooldownHit(target ResolverTarget, platform, link string, settings ResolverCooldownSettings) bool {
	now := handler.now()
	targetKey := target.TargetType + ":" + target.TargetID
	keys := make([]struct {
		key      string
		enabled  bool
		duration time.Duration
	}, 0, 2)
	keys = append(keys,
		struct {
			key      string
			enabled  bool
			duration time.Duration
		}{targetKey + ":platform:" + platform, settings.SamePlatformEnabled, time.Duration(settings.SamePlatformSeconds) * time.Second},
		struct {
			key      string
			enabled  bool
			duration time.Duration
		}{targetKey + ":link:" + platform + ":" + link, settings.SameLinkEnabled, time.Duration(settings.SameLinkSeconds) * time.Second},
	)
	handler.resolverMu.Lock()
	defer handler.resolverMu.Unlock()
	if len(handler.resolverCooldowns) > 4096 {
		retention := time.Duration(max(settings.SameLinkSeconds, settings.SamePlatformSeconds)) * time.Second
		for key, recordedAt := range handler.resolverCooldowns {
			if now.Sub(recordedAt) >= retention {
				delete(handler.resolverCooldowns, key)
			}
		}
	}
	for _, item := range keys {
		if item.enabled && now.Sub(handler.resolverCooldowns[item.key]) < item.duration {
			return true
		}
	}
	for _, item := range keys {
		if item.enabled {
			handler.resolverCooldowns[item.key] = now
		}
	}
	return false
}

func genericResolverMedia(update Update) ResolverMediaPlan {
	service := StringScalar(update["service"])
	if service != "image" && service != "image_text" && service != "repost" {
		return ResolverMediaPlan{}
	}
	return ResolverImagePlan(update, nil, "image")
}

func ResolverImagePlan(update Update, headers map[string]string, prefix string) ResolverMediaPlan {
	images := resolverUpdateImages(update)
	if len(images) == 0 {
		return ResolverMediaPlan{}
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "image"
	}
	sources := make([]ResolverMediaSource, 0, len(images))
	for index, imageURL := range images {
		sources = append(sources, ResolverMediaSource{Kind: "image", URLs: []string{imageURL}, Headers: headers, FileName: prefix + "-" + StringScalar(index+1) + ".jpg"})
	}
	return ResolverMediaPlan{Sources: sources}
}

func resolverUpdateImages(update Update) []string {
	seen := map[string]bool{}
	result := make([]string, 0)
	appendImages := func(value any) {
		for _, image := range MapSliceValue(value) {
			for _, candidate := range append([]string{StringScalar(image["url"])}, MediaItemCandidates(image["candidates"])...) {
				if strings.HasPrefix(candidate, "https://") && !seen[candidate] {
					seen[candidate] = true
					result = append(result, candidate)
					break
				}
			}
		}
	}
	appendImages(update["images"])
	if original := MapValue(update["original"]); original != nil {
		appendImages(original["images"])
	}
	return result
}

func (handler *Handler) sendResolverFailure(ctx context.Context, event *rayleabot.EventContext, message string) {
	if ctx.Err() != nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, resolverFailureSendTimeout)
	defer cancel()
	_, _ = handler.hostActions(event).MessageSend(sendCtx, rayleabot.MessageSendRequest{
		TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(EnsureSentence(message))}},
	})
}
