package weibo

import (
	"strconv"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

const weiboRenderResourceReferer = "https://weibo.com/"

const maxWeiboRenderResourceFallbacks = 4

func prepareWeiboUpdateResources(data map[string]any) []plugin.RenderResource {
	if data == nil {
		return nil
	}
	type mediaSection struct {
		items []map[string]any
	}
	sections := []mediaSection{{items: plugin.MapSliceValue(data["media_items"])}}
	if original := plugin.MapValue(data["original"]); original != nil {
		sections = append(sections, mediaSection{items: plugin.MapSliceValue(original["media_items"])})
	}

	resources := make([]plugin.RenderResource, 0, 15)
	for _, section := range sections {
		for _, item := range section.items {
			sourceURL := strings.TrimSpace(plugin.StringScalar(item["url"]))
			if !strings.HasPrefix(sourceURL, "https://") {
				continue
			}
			candidates := weiboMediaCandidateURLs(sourceURL)
			item["url"] = plugin.FirstText(item["fallback"], "assets/grid.svg")
			if len(candidates) == 0 {
				continue
			}

			resourceID := "weibo-media-" + strconv.Itoa(len(resources))
			item["resource_id"] = resourceID
			resource := plugin.RenderResource{
				ID:      resourceID,
				URL:     candidates[0],
				Referer: weiboRenderResourceReferer,
			}
			if len(candidates) > 1 {
				end := len(candidates)
				if end > maxWeiboRenderResourceFallbacks+1 {
					end = maxWeiboRenderResourceFallbacks + 1
				}
				resource.FallbackURLs = append([]string(nil), candidates[1:end]...)
			}
			resources = append(resources, resource)
		}
	}
	return resources
}

func weiboMediaCandidateURLs(sourceURL string) []string {
	parsed, referer, err := plugin.ValidateAvatarSourceURL(sourceURL, avatarPolicy())
	if err != nil || referer != weiboRenderResourceReferer {
		return nil
	}

	sizes := []string{"large", "mw2000", "mw1024", "mw690", "bmiddle"}
	result := make([]string, 0, len(sizes)+1)
	seen := make(map[string]struct{}, len(sizes)+1)
	for _, size := range sizes {
		candidate := parsed.String()
		if weiboOrjSizePattern.MatchString(parsed.Path) {
			clone := *parsed
			clone.Path = weiboOrjSizePattern.ReplaceAllString(parsed.Path, "/"+size+"/")
			clone.RawPath = ""
			candidate = clone.String()
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}
	if original := parsed.String(); len(result) < maxWeiboRenderResourceFallbacks+1 {
		if _, exists := seen[original]; !exists {
			result = append(result, original)
		}
	}
	return result
}
