package weibo

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const (
	weiboCardMediaTimeout  = 12 * time.Second
	weiboCardImageTimeout  = 6
	weiboCardImageMaxBytes = 320 << 10
	weiboCardMediaMaxBytes = 512 << 10
	weiboCardMediaMaxItems = 9
	weiboCardConcurrency   = 3
)

func (session *session) PrepareResolverCard(ctx context.Context, card plugin.CardRequest) plugin.CardRequest {
	return session.prepareCardMedia(ctx, card)
}

func (session *session) PrepareUpdateCard(ctx context.Context, card plugin.CardRequest) plugin.CardRequest {
	return session.prepareCardMedia(ctx, card)
}

// prepareCardMedia resolves CDN previews with bounded http.request actions so
// render.image only receives local template assets or inline images.
func (session *session) prepareCardMedia(ctx context.Context, card plugin.CardRequest) plugin.CardRequest {
	if len(card.Resources) == 0 {
		return card
	}
	resources := make(map[string]plugin.RenderResource, len(card.Resources))
	for _, resource := range card.Resources {
		resources[resource.ID] = resource
	}
	items := weiboCardMediaItems(card.Data)
	resourceIDs := make([]string, len(items))
	for index, item := range items {
		resourceIDs[index] = plugin.StringScalar(item["resource_id"])
		delete(item, "resource_id")
	}
	if len(items) > weiboCardMediaMaxItems {
		items = items[:weiboCardMediaMaxItems]
		resourceIDs = resourceIDs[:weiboCardMediaMaxItems]
	}

	mediaCtx, cancel := context.WithTimeout(ctx, weiboCardMediaTimeout)
	defer cancel()
	type fetchedImage struct {
		index int
		url   string
	}
	fetched := make([]fetchedImage, 0, len(items))
	totalBytes := 0
	var mu sync.Mutex
	var wait sync.WaitGroup
	gate := make(chan struct{}, weiboCardConcurrency)
	for index := range items {
		resource, ok := resources[resourceIDs[index]]
		if !ok {
			continue
		}
		wait.Add(1)
		go func(index int, resource plugin.RenderResource) {
			defer wait.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-mediaCtx.Done():
				return
			}
			for _, candidate := range weiboCardImageCandidates(resource) {
				dataURL, size, err := plugin.ResolveAvatarDataURLLimited(
					mediaCtx,
					session.actions,
					candidate,
					weiboCardImageTimeout,
					weiboCardImageMaxBytes,
					avatarPolicy(),
				)
				if err != nil {
					continue
				}
				mu.Lock()
				if totalBytes+size <= weiboCardMediaMaxBytes {
					totalBytes += size
					fetched = append(fetched, fetchedImage{index: index, url: dataURL})
				}
				mu.Unlock()
				return
			}
		}(index, resource)
	}
	wait.Wait()
	for _, image := range fetched {
		items[image.index]["url"] = image.url
	}
	card.Resources = nil
	return card
}

func weiboCardMediaItems(data map[string]any) []map[string]any {
	items := append([]map[string]any(nil), plugin.MapSliceValue(data["media_items"])...)
	if original := plugin.MapValue(data["original"]); original != nil {
		items = append(items, plugin.MapSliceValue(original["media_items"])...)
	}
	return items
}

func weiboCardImageCandidates(resource plugin.RenderResource) []string {
	candidates := append([]string{resource.URL}, resource.FallbackURLs...)
	sort.SliceStable(candidates, func(left, right int) bool {
		return weiboCardImageRank(candidates[left]) < weiboCardImageRank(candidates[right])
	})
	return candidates
}

func weiboCardImageRank(rawURL string) int {
	for rank, marker := range []string{"/mw690/", "/bmiddle/", "/mw1024/", "/large/", "/mw2000/"} {
		if strings.Contains(strings.ToLower(rawURL), marker) {
			return rank
		}
	}
	return 5
}
