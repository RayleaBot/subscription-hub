package netease_music

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
		if host == "music.163.com" {
			if id := parsed.Query().Get("id"); plugin.SafeSubjectID(id) != "" {
				return plugin.SafeSubjectID(id)
			}
			fragment, _ := url.Parse(strings.TrimPrefix(parsed.Fragment, "/"))
			if fragment != nil {
				return plugin.SafeSubjectID(fragment.Query().Get("id"))
			}
		}
	}
	return plugin.SafeSubjectID(value)
}
