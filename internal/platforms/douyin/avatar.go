package douyin

import (
	"net/url"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func avatarPolicy() plugin.AvatarPolicy {
	return plugin.AvatarPolicy{Validate: func(parsed *url.URL) (string, bool) {
		host := strings.ToLower(parsed.Hostname())
		return douyinRenderResourceReferer, host == "douyinpic.com" || strings.HasSuffix(host, ".douyinpic.com")
	}}
}
