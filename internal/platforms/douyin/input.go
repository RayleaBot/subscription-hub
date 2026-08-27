package douyin

import (
	"net/url"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func subjectIDFromInput(value string) string {
	value = strings.TrimSpace(value)
	for _, raw := range plugin.URLPattern.FindAllString(value, -1) {
		parsed, err := url.Parse(strings.TrimRight(raw, "。），,)"))
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		parts := plugin.PathParts(parsed.Path)
		if strings.Contains(host, "douyin.com") || strings.HasSuffix(host, "iesdouyin.com") || strings.HasSuffix(host, "amemv.com") {
			for index, part := range parts {
				if (part == "user" || part == "video" || part == "note") && index+1 < len(parts) {
					return plugin.SafeSubjectID(parts[index+1])
				}
			}
		}
	}
	return plugin.SafeSubjectID(value)
}
