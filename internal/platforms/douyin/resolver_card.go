package douyin

import (
	"context"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const (
	douyinResolverCardMediaTimeout  = 4 * time.Second
	douyinResolverCardImageTimeout  = 2
	douyinResolverCardImageMaxBytes = 256 << 10
	douyinResolverCardMediaMaxBytes = 512 << 10
	douyinResolverCardMediaMaxItems = 3
)

// PrepareResolverCard keeps Douyin CDN fetching outside render.image. A slow
// cover must not keep the render action open long enough to break the event's
// protocol session; unavailable covers fall back to the template asset.
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
	totalBytes := 0
	processed := 0
	for _, item := range plugin.MapSliceValue(card.Data["media_items"]) {
		resourceID := plugin.StringScalar(item["resource_id"])
		resource, ok := resources[resourceID]
		delete(item, "resource_id")
		if !ok || processed >= douyinResolverCardMediaMaxItems || mediaCtx.Err() != nil {
			continue
		}
		processed++
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
			if err != nil || totalBytes+size > douyinResolverCardMediaMaxBytes {
				continue
			}
			item["url"] = dataURL
			totalBytes += size
			break
		}
	}
	card.Resources = nil
	return card
}
