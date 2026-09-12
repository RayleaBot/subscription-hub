package douyin

import (
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

var catalog = plugin.NewServiceCatalog(
	[]string{"video", "image_text", "live"},
	map[string]string{"all": "全部", "video": "视频", "image_text": "图文", "live": "直播"},
	map[string]string{"全部": "all", "全量": "all", "所有": "all", "视频": "video", "图文": "image_text", "图片": "image_text", "直播": "live"})

func New() plugin.Platform {
	return plugin.Platform{
		PreviewPrefixes: []string{"douyin", "抖音"},
		ID:              "douyin", Name: "抖音", SubjectLabel: "sec_uid", ListTitle: "抖音订阅列表", AllListTitle: "全部抖音订阅列表",
		Commands: map[string]string{"订阅抖音推送": "add", "取消抖音推送": "remove", "抖音搜索用户": "search", "抖音订阅列表": "list", "全部抖音订阅列表": "list_all"}, Services: catalog, ParseSubject: subjectIDFromInput,
		NewSession:     func(actions plugin.SourceActions) plugin.Session { return &session{actions: actions} },
		PreviewAliases: []string{"douyin", "抖音"}, PreviewDefault: "video", PreviewInputs: nil, Baseline: &plugin.BaselinePolicy{KeyPrefix: "source:douyin:feed:initialized:", Timestamp: true, ExemptServices: []string{"live"}}, Avatar: avatarPolicy(), Cleanup: func(item plugin.Subscription) []plugin.KVSelector {
			uid := strings.TrimSpace(item.UID)
			return []plugin.KVSelector{{Prefix: "source:douyin:resolved_uid:" + uid}, {Prefix: "source:douyin:live:" + uid}, {Prefix: "source:douyin:zero_posts:" + uid}}
		},
		Account: &plugin.AccountAdapter{Validate: ValidateAccount, NewQRProvider: NewAccountQRProvider},
	}
}
