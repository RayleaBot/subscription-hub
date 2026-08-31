package douyin

import (
	"context"
	"sync"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const (
	// douyinpic 图片 CDN 实测响应 3-7 秒（首包慢、与登录态无关），
	// 单候选超时与总预算都需要留足余量，否则封面全部落入占位图。
	douyinResolverCardMediaTimeout  = 20 * time.Second
	douyinResolverCardImageTimeout  = 8
	douyinResolverCardImageMaxBytes = 256 << 10
	douyinResolverCardMediaMaxBytes = 512 << 10
	douyinResolverCardMediaMaxItems = 3
)

// PrepareResolverCard keeps Douyin CDN fetching outside render.image. A slow
// cover must not keep the render action open long enough to break the event's
// protocol session; unavailable covers fall back to the template asset.
// 封面并发预取：douyinpic 单图 3-7 秒，串行会挤占后续媒体发送的事件预算。
func (session *session) PrepareResolverCard(ctx context.Context, card plugin.CardRequest) plugin.CardRequest {
	if len(card.Resources) == 0 {
		return card
	}
	resources := make(map[string]plugin.RenderResource, len(card.Resources))
	for _, resource := range card.Resources {
		resources[resource.ID] = resource
	}

	mediaCtx, cancel := context.WithTimeout(ctx, douyinResolverCardMediaTimeout)
	defer cancel()

	allItems := plugin.MapSliceValue(card.Data["media_items"])
	resourceIDs := make([]string, len(allItems))
	for index, item := range allItems {
		resourceIDs[index] = plugin.StringScalar(item["resource_id"])
		delete(item, "resource_id")
	}
	items := allItems
	if len(items) > douyinResolverCardMediaMaxItems {
		items = items[:douyinResolverCardMediaMaxItems]
	}
	type fetchedCover struct {
		index int
		url   string
	}
	var mu sync.Mutex
	fetched := make([]fetchedCover, 0, len(items))
	totalBytes := 0
	var wait sync.WaitGroup
	for index := range items {
		resourceID := resourceIDs[index]
		resource, ok := resources[resourceID]
		if !ok {
			continue
		}
		wait.Add(1)
		go func(index int, resource plugin.RenderResource) {
			defer wait.Done()
			candidates := append([]string{resource.URL}, resource.FallbackURLs...)
			for _, candidate := range candidates {
				dataURL, size, err := plugin.ResolveAvatarDataURLLimited(
					mediaCtx,
					session.actions,
					candidate,
					douyinResolverCardImageTimeout,
					douyinResolverCardImageMaxBytes,
					avatarPolicy(),
				)
				if err != nil {
					continue
				}
				mu.Lock()
				if totalBytes+size <= douyinResolverCardMediaMaxBytes {
					totalBytes += size
					fetched = append(fetched, fetchedCover{index: index, url: dataURL})
				}
				mu.Unlock()
				return
			}
		}(index, resource)
	}
	wait.Wait()
	for _, cover := range fetched {
		items[cover.index]["url"] = cover.url
	}
	card.Resources = nil
	return card
}
