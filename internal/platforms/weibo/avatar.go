package weibo

import (
	"net/url"
	"strings"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func avatarPolicy() plugin.AvatarPolicy {
	return plugin.AvatarPolicy{Validate: func(parsed *url.URL) (string, bool) {
		switch strings.ToLower(parsed.Hostname()) {
		case "tva1.sinaimg.cn", "tva2.sinaimg.cn", "tva3.sinaimg.cn", "tva4.sinaimg.cn",
			"tvax1.sinaimg.cn", "tvax2.sinaimg.cn", "tvax3.sinaimg.cn", "tvax4.sinaimg.cn",
			"wx1.sinaimg.cn", "wx2.sinaimg.cn", "wx3.sinaimg.cn", "wx4.sinaimg.cn":
			return "https://weibo.com/", true
		}
		return "", false
	}}
}
