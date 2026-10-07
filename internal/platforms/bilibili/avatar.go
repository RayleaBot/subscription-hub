package bilibili

import (
	"net/url"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func avatarPolicy() plugin.AvatarPolicy {
	return plugin.AvatarPolicy{
		Validate: func(parsed *url.URL) (string, bool) {
			switch strings.ToLower(parsed.Hostname()) {
			case "i0.hdslb.com", "i1.hdslb.com", "i2.hdslb.com":
				path := parsed.EscapedPath()
				return "https://www.bilibili.com/", parsed.RawQuery == "" && (strings.HasPrefix(path, "/bfs/face/") || strings.HasPrefix(path, "/bfs/garb/"))
			}
			return "", false
		},
		Candidates: func(parsed *url.URL) []string {
			compact := *parsed
			if !strings.Contains(compact.Path[strings.LastIndex(compact.Path, "/")+1:], "@") {
				compact.Path += bilibiliSearchAvatarSuffix
				compact.RawPath = ""
			}
			return []string{compact.String(), parsed.String()}
		},
	}
}
