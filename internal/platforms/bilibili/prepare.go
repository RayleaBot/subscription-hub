package bilibili

import (
	"context"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func prepareUpdate(ctx context.Context, actions plugin.SourceActions, update map[string]any) (map[string]any, error) {
	service := plugin.StringScalar(update["service"])
	prepared := plugin.CloneJSONMap(update)
	ref := parseBilibiliPreviewURL(plugin.StringScalar(prepared["url"]))
	needsImageTextDetail := service == "image_text" && plugin.StringScalar(prepared["summary"]) == "" && plugin.StringScalar(prepared["summary_html"]) == ""
	needsOriginal := service == "repost" && plugin.MapValue(prepared["original"]) == nil
	if ref != nil && (needsImageTextDetail || needsOriginal) && (ref.Kind == "opus" || ref.Kind == "dynamic") {
		if detailed, err := fetchBilibiliPreview(ctx, actions, ref); err == nil {
			if needsImageTextDetail {
				mergeMissingBilibiliDetail(prepared, detailed)
			}
			if needsOriginal {
				if original := plugin.MapValue(detailed["original"]); original != nil {
					prepared["original"] = original
				}
				for _, key := range []string{"summary", "summary_html"} {
					if plugin.StringScalar(prepared[key]) == "" && plugin.StringScalar(detailed[key]) != "" {
						prepared[key] = detailed[key]
					}
				}
			}
		} else {
			return prepared, err
		}
	}
	return prepared, nil
}

func mergeMissingBilibiliDetail(target, source map[string]any) {
	for _, key := range []string{"summary", "summary_html"} {
		if plugin.StringScalar(target[key]) == "" && plugin.StringScalar(source[key]) != "" {
			target[key] = source[key]
		}
	}
	if plugin.MapValue(target["topic"]) == nil && plugin.MapValue(source["topic"]) != nil {
		target["topic"] = source["topic"]
	}
	if len(plugin.ImageMaps(target["images"], 9)) == 0 && len(plugin.ImageMaps(source["images"], 9)) > 0 {
		target["images"] = source["images"]
	}
}
