package bilibili

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
		if (host == "space.bilibili.com" || host == "m.bilibili.com") && len(parts) > 0 && plugin.Digits(parts[0]) != "" {
			return parts[0]
		}
	}
	return plugin.Digits(value)
}
