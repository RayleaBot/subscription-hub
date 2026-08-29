package douyin

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

// douyinpic 区域主机形如 p3-pc-sign.douyinpic.com；同一路径与签名的资源在
// p3/p6/p9/p26… 区域 CDN 都有镜像，换主机重试不改变签名有效性。
var douyinpicHostPattern = regexp.MustCompile(`^p([0-9]+)-pc(-sign)?\.douyinpic\.com$`)

func avatarPolicy() plugin.AvatarPolicy {
	return plugin.AvatarPolicy{
		Validate: func(parsed *url.URL) (string, bool) {
			host := strings.ToLower(parsed.Hostname())
			return douyinRenderResourceReferer, host == "douyinpic.com" || strings.HasSuffix(host, ".douyinpic.com")
		},
		Candidates: func(parsed *url.URL) []string {
			return append([]string{parsed.String()}, douyinpicRegionMirrors(parsed)...)
		},
	}
}

// douyinpicRegionMirrors 把 douyinpic 主机前缀换到其他区域，返回备用镜像地址。
func douyinpicRegionMirrors(parsed *url.URL) []string {
	host := strings.ToLower(parsed.Hostname())
	match := douyinpicHostPattern.FindStringSubmatch(host)
	if match == nil {
		return nil
	}
	alts := make([]string, 0, 4)
	seen := map[string]bool{host: true}
	for _, region := range []string{"6", "9", "26", "11"} {
		mirrorHost := "p" + region + "-pc" + match[2] + ".douyinpic.com"
		if seen[mirrorHost] {
			continue
		}
		seen[mirrorHost] = true
		cp := *parsed
		cp.Host = mirrorHost
		alts = append(alts, cp.String())
	}
	return alts
}
