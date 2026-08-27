package plugin

import (
	"strconv"
	"strings"
)

const douyinRenderResourceReferer = "https://www.douyin.com/"

func prepareDouyinUpdateResources(data map[string]any) []pluginRenderImageResource {
	if data == nil {
		return nil
	}
	resources := make([]pluginRenderImageResource, 0, 15)
	for _, item := range mapSliceValue(data["media_items"]) {
		sourceURL := strings.TrimSpace(stringScalar(item["url"]))
		if !strings.HasPrefix(sourceURL, "https://") {
			continue
		}
		parsed, referer, err := validateAvatarSourceURL(sourceURL)
		if err != nil || referer != douyinRenderResourceReferer {
			item["url"] = firstText(item["fallback"], "assets/grid.svg")
			continue
		}
		item["url"] = firstText(item["fallback"], "assets/grid.svg")
		resourceID := "douyin-media-" + strconv.Itoa(len(resources))
		item["resource_id"] = resourceID
		resources = append(resources, pluginRenderImageResource{
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
