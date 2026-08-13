package plugin

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	bilibiliUserCardTemplate           = "bilibili-user-card"
	bilibiliRenderActionTimeout        = 35 * time.Second
	bilibiliUpdateAvatarTimeoutSeconds = 3
	bilibiliUpdateAvatarTotalTimeout   = 4 * time.Second
)

type bilibiliAvatarCacheEntry struct {
	ready chan struct{}
	value string
}

type bilibiliAvatarCache struct {
	entries sync.Map
}

// buildBilibiliUserCardData 生成订阅/取消订阅 UP 主资料卡片的渲染输入。
// action 取值：subscribed（新订阅）、updated（更新订阅）、unsubscribed（取消订阅）。
func buildBilibiliUserCardData(action string, item subscription, user bilibiliUser, cardServices []string) map[string]any {
	name := firstText(user.Name, item.Name, item.UID)
	uid := firstText(user.UID, item.UID)
	levelIcon := ""
	if user.Level > 0 || user.Senior {
		levelIcon = bilibiliLevelIcon(user.Level, user.Senior)
	}
	return map[string]any{
		"action":         action,
		"subtitle":       "Bilibili · 订阅中心",
		"platform":       platformName(item.Platform),
		"user":           map[string]any{"name": name, "uid": uid, "avatar": firstText(user.AvatarURL, item.AvatarURL), "fans": user.Fans, "sign": truncateRunes(user.Sign, 80)},
		"uid_text":       uidText(uid),
		"fans_text":      fansText(user.Fans),
		"videos_text":    videosText(user.Videos),
		"level_icon":     levelIcon,
		"verify_text":    user.Verify,
		"verify_org":     user.VerifyOrg,
		"services_text":  servicesText(cardServices, item.Platform),
		"service_labels": serviceLabels(cardServices, item.Platform),
		"target_text":    userCardTargetText(action, item),
	}
}

// sendBilibiliUserCard 渲染并发送 UP 主资料卡片；渲染失败时降级为原文字回复。
func sendBilibiliUserCard(ctx context.Context, event *rayleabot.EventContext, data map[string]any, fallbackMessage string) error {
	return sendRenderedCard(ctx, event, bilibiliUserCardTemplate, data, fallbackMessage)
}

// inlineBilibiliCardAvatar 把资料卡片头像内联为 dataURL，避免渲染时远程图片与截图竞态
// （远程加载慢时截图抓到破图，卡住时渲染超时降级为文字）。失败返回空，模板回退默认头像。
func inlineBilibiliCardAvatar(ctx context.Context, actions pluginActions, sourceURL string) string {
	return inlineBilibiliCardAvatarWithTimeout(ctx, actions, sourceURL, bilibiliSearchAvatarTimeoutSeconds)
}

func inlineBilibiliCardAvatarWithTimeout(ctx context.Context, actions pluginActions, sourceURL string, timeoutSeconds int) string {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" || isBilibiliDefaultAvatarURL(sourceURL) {
		return ""
	}
	requestURL, trusted := bilibiliSearchAvatarURL(sourceURL)
	if !trusted {
		return ""
	}
	seen := make(map[string]struct{}, 2)
	for _, candidate := range []string{requestURL, sourceURL} {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		if dataURL, _, err := resolveAvatarDataURLWithTimeout(ctx, actions, candidate, timeoutSeconds); err == nil {
			return dataURL
		}
	}
	return ""
}

func (cache *bilibiliAvatarCache) resolve(ctx context.Context, actions pluginActions, sourceURL string) string {
	if ctx.Err() != nil {
		return ""
	}
	if cache == nil {
		return inlineBilibiliCardAvatarWithTimeout(ctx, actions, sourceURL, bilibiliUpdateAvatarTimeoutSeconds)
	}
	entry := &bilibiliAvatarCacheEntry{ready: make(chan struct{})}
	actual, loaded := cache.entries.LoadOrStore(strings.TrimSpace(sourceURL), entry)
	if loaded {
		cached := actual.(*bilibiliAvatarCacheEntry)
		select {
		case <-cached.ready:
			return cached.value
		case <-ctx.Done():
			return ""
		}
	}
	entry.value = inlineBilibiliCardAvatarWithTimeout(ctx, actions, sourceURL, bilibiliUpdateAvatarTimeoutSeconds)
	close(entry.ready)
	return entry.value
}

// inlineBilibiliUpdateAvatars 并发内联动态卡片中的作者、原作者与订阅人头像。
// 内联失败时对应字段置空，模板回退到内置默认头像，避免远程图片与截图竞态。
func inlineBilibiliUpdateAvatars(ctx context.Context, actions pluginActions, data map[string]any) {
	avatarCtx, cancel := context.WithTimeout(ctx, bilibiliUpdateAvatarTotalTimeout)
	defer cancel()
	inlineBilibiliUpdateAvatarsWithCache(avatarCtx, actions, data, &bilibiliAvatarCache{})
}

func inlineBilibiliUpdateAvatarsWithCache(ctx context.Context, actions pluginActions, data map[string]any, cache *bilibiliAvatarCache) {
	type avatarField struct {
		object map[string]any
		key    string
	}
	fields := make([]avatarField, 0, 4)
	if author := mapValue(data["author"]); author != nil {
		fields = append(fields, avatarField{author, "avatar"})
	}
	if original := mapValue(data["original"]); original != nil {
		if author := mapValue(original["author"]); author != nil {
			fields = append(fields, avatarField{author, "avatar"})
		}
	}
	for _, cardMap := range mapSliceValue(data["subscriber_cards"]) {
		fields = append(fields, avatarField{cardMap, "avatar_url"})
	}
	var wait sync.WaitGroup
	for _, field := range fields {
		sourceURL := stringScalar(field.object[field.key])
		if !strings.HasPrefix(sourceURL, "https://") && !strings.HasPrefix(sourceURL, "http://") {
			// 本地资源（如预览示例的 assets 路径）保持原样。
			continue
		}
		wait.Add(1)
		go func(field avatarField, sourceURL string) {
			defer wait.Done()
			field.object[field.key] = cache.resolve(ctx, actions, sourceURL)
		}(field, sourceURL)
	}
	wait.Wait()
}

// sendRenderedCard 渲染指定模板并发送图片；渲染失败时发送指定回退文本。
func sendRenderedCard(ctx context.Context, event *rayleabot.EventContext, template string, data map[string]any, fallbackMessage string) error {
	imagePath, err := renderBilibiliCardImage(ctx, event.Actions(), template, data, fallbackMessage)
	if err != nil || imagePath == "" {
		return event.SendText(fallbackMessage)
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func renderBilibiliCardImage(ctx context.Context, actions pluginActions, template string, data map[string]any, fallbackMessage string) (string, error) {
	renderCtx, cancel := context.WithTimeout(ctx, bilibiliRenderActionTimeout)
	defer cancel()
	result, err := actions.RenderImage(renderCtx, rayleabot.RenderImageRequest{
		Template: template, Data: data, Theme: "default", Output: "png", FallbackText: fallbackMessage,
	})
	return stringScalar(result["image_path"]), err
}

func serviceLabels(values []string, platform string) []string {
	catalog := services[platform]
	values = normalizeServices(values, platform)
	labels := make([]string, 0, len(values))
	for _, value := range values {
		labels = append(labels, catalog.names[value])
	}
	return labels
}

func fansText(fans int) string {
	if fans <= 0 {
		return ""
	}
	if fans >= 10000 {
		return "粉丝 " + strconv.FormatFloat(float64(fans)/10000, 'f', 1, 64) + "万"
	}
	return "粉丝 " + strconv.Itoa(fans)
}

func userCardTargetText(action string, item subscription) string {
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
