package weibo

import (
	"context"
	"sort"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func (session *session) PrepareResolverCard(_ context.Context, card plugin.CardRequest) plugin.CardRequest {
	return prepareWeiboCardResources(card)
}

func (session *session) PrepareUpdateCard(_ context.Context, card plugin.CardRequest) plugin.CardRequest {
	return prepareWeiboCardResources(card)
}

// 媒体通过宿主独立资源预取，保留全部资源 ID，避免内联图片挤占卡片正文限额。
func prepareWeiboCardResources(card plugin.CardRequest) plugin.CardRequest {
	resources := append([]plugin.RenderResource(nil), card.Resources...)
	for index, resource := range resources {
		candidates := weiboCardImageCandidates(resource)
		resources[index].URL = candidates[0]
		resources[index].FallbackURLs = append([]string(nil), candidates[1:]...)
	}
	card.Resources = resources
	return card
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
