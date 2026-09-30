package bilibili

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const bilibiliRenderResourceReferer = "https://www.bilibili.com/"

func prepareBilibiliUpdateResources(data map[string]any) []plugin.RenderResource {
	items := append([]map[string]any(nil), plugin.MapSliceValue(data["media_items"])...)
	if original := plugin.MapValue(data["original"]); original != nil {
		items = append(items, plugin.MapSliceValue(original["media_items"])...)
	}
	resources := make([]plugin.RenderResource, 0, len(items))
	for _, item := range items {
		sourceURL := bilibiliImageURL(item["url"])
		if sourceURL == "" {
			continue
		}
		item["url"] = plugin.FirstText(item["fallback"], "assets/grid.svg")
		candidates := bilibiliMediaCandidateURLs(sourceURL)
		if len(candidates) == 0 {
			continue
		}
		id := "bilibili-media-" + strconv.Itoa(len(resources))
		item["resource_id"] = id
		resources = append(resources, plugin.RenderResource{
			ID: id, URL: candidates[0], FallbackURLs: candidates[1:], Referer: bilibiliRenderResourceReferer,
		})
	}
	return resources
}

func bilibiliMediaCandidateURLs(sourceURL string) []string {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil
	}
	result := []string{parsed.String()}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Port() != "" || (host != "i0.hdslb.com" && host != "i1.hdslb.com" && host != "i2.hdslb.com") {
		return result
	}
	// 图片 CDN 的 i0/i1/i2 镜像使用同一文件路径，切换主机不改变图片规格。
	for _, mirror := range []string{"i0.hdslb.com", "i1.hdslb.com", "i2.hdslb.com"} {
		if mirror == host {
			continue
		}
		candidate := *parsed
		candidate.Host = mirror
		result = append(result, candidate.String())
	}
	return result
}
