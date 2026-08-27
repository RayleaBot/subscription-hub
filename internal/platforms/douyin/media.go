package douyin

import (
	"strconv"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const douyinRenderResourceReferer = "https://www.douyin.com/"

func prepareDouyinUpdateResources(data map[string]any) []plugin.RenderResource {
	if data == nil {
		return nil
	}
	resources := make([]plugin.RenderResource, 0, 15)
	for _, item := range plugin.MapSliceValue(data["media_items"]) {
		sourceURL := strings.TrimSpace(plugin.StringScalar(item["url"]))
		if !strings.HasPrefix(sourceURL, "https://") {
			continue
		}
		parsed, referer, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy())
		if err != nil || referer != douyinRenderResourceReferer {
			item["url"] = plugin.FirstText(item["fallback"], "assets/grid.svg")
			continue
		}
		item["url"] = plugin.FirstText(item["fallback"], "assets/grid.svg")
		resourceID := "douyin-media-" + strconv.Itoa(len(resources))
		item["resource_id"] = resourceID
		resources = append(resources, plugin.RenderResource{
			ID:      resourceID,
			URL:     parsed.String(),
			Referer: douyinRenderResourceReferer,
		})
		if len(resources) >= 9 {
			break
		}
	}
	return resources
}
