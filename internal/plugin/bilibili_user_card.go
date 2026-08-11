package plugin

import (
	"context"
	"strconv"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const bilibiliUserCardTemplate = "bilibili-user-card"

// buildBilibiliUserCardData 生成订阅/取消订阅 UP 主资料卡片的渲染输入。
// action 取值：subscribed（新订阅）、updated（更新订阅）、unsubscribed（取消订阅）。
func buildBilibiliUserCardData(action string, item subscription, user bilibiliUser, cardServices []string) map[string]any {
	name := firstText(user.Name, item.Name, item.UID)
	uid := firstText(user.UID, item.UID)
	return map[string]any{
		"action":         action,
		"subtitle":       "Bilibili · 订阅中心",
		"platform":       platformName(item.Platform),
		"user":           map[string]any{"name": name, "uid": uid, "avatar": firstText(user.AvatarURL, item.AvatarURL), "fans": user.Fans, "sign": truncateRunes(user.Sign, 80)},
		"uid_text":       uidText(uid),
		"fans_text":      fansText(user.Fans),
		"services_text":  servicesText(cardServices, item.Platform),
		"service_labels": serviceLabels(cardServices, item.Platform),
		"target_text":    userCardTargetText(action, item),
	}
}

// sendBilibiliUserCard 渲染并发送 UP 主资料卡片；渲染失败时降级为原文字回复。
func sendBilibiliUserCard(ctx context.Context, event *rayleabot.EventContext, data map[string]any, fallbackMessage string) error {
	return sendBilibiliCard(ctx, event, bilibiliUserCardTemplate, data, fallbackMessage)
}

// sendBilibiliCard 渲染指定模板并发送图片；渲染失败时降级为原文字回复。
func sendBilibiliCard(ctx context.Context, event *rayleabot.EventContext, template string, data map[string]any, fallbackMessage string) error {
	return sendBilibiliCardWithRenderFallback(ctx, event, template, data, nil, fallbackMessage)
}

// sendBilibiliCardWithRenderFallback 在内联资源渲染失败时使用远程资源数据重试一次。
func sendBilibiliCardWithRenderFallback(ctx context.Context, event *rayleabot.EventContext, template string, data, renderFallback map[string]any, fallbackMessage string) error {
	imagePath, err := renderBilibiliCardImage(ctx, event.Actions(), template, data, renderFallback, fallbackMessage)
	if err != nil || imagePath == "" {
		return event.SendText(fallbackMessage)
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func renderBilibiliCardImage(ctx context.Context, actions pluginActions, template string, data, renderFallback map[string]any, fallbackMessage string) (string, error) {
	render := func(input map[string]any) (rayleabot.ActionResult, error) {
		return actions.RenderImage(ctx, rayleabot.RenderImageRequest{
			Template: template, Data: input, Theme: "default", Output: "png", FallbackText: fallbackMessage,
		})
	}
	result, err := render(data)
	imagePath := stringScalar(result["image_path"])
	if (err != nil || imagePath == "") && renderFallback != nil {
		result, err = render(renderFallback)
		imagePath = stringScalar(result["image_path"])
	}
	return imagePath, err
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
