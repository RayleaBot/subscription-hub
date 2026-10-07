package weibo

import (
	"net/url"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
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
		if strings.HasSuffix(host, "weibo.com") || strings.HasSuffix(host, "weibo.cn") {
			if len(parts) > 1 && (parts[0] == "u" || parts[0] == "profile") {
				return plugin.SafeSubjectID(parts[1])
			}
			if len(parts) > 0 {
				return plugin.SafeSubjectID(parts[0])
			}
		}
	}
	return plugin.SafeSubjectID(value)
}
