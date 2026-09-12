package bilibili

import (
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

var catalog = plugin.NewServiceCatalog(
	[]string{"live", "video", "image_text", "article", "repost"},
	map[string]string{"all": "全部", "live": "直播", "video": "视频", "image_text": "图文", "article": "专栏", "repost": "转发"},
	map[string]string{"全部": "all", "全量": "all", "所有": "all", "直播": "live", "视频": "video", "图文": "image_text", "动态": "image_text", "文章": "article", "专栏": "article", "转发": "repost"})

func New() plugin.Platform {
	return plugin.Platform{
		PreviewPrefixes: []string{"b站"},
		ID:              "bilibili", Name: "Bilibili", SubjectLabel: "UID", ListTitle: "Bilibili 订阅列表", AllListTitle: "全部 Bilibili 订阅列表",
		Commands: map[string]string{"订阅b站推送": "add", "取消b站推送": "remove", "b站搜索up": "search", "b站搜索UP": "search", "B站搜索up": "search", "B站搜索UP": "search", "b站订阅列表": "list", "全部b站订阅列表": "list_all"}, Services: catalog, ParseSubject: subjectIDFromInput,
		NewSession:     func(actions plugin.SourceActions) plugin.Session { return &session{actions: actions} },
		PreviewAliases: []string{"bilibili", "bili", "b站"}, PreviewDefault: "video", PreviewInputs: nil, Baseline: &plugin.BaselinePolicy{KeyPrefix: "source:bilibili:dynamic:initialized:", Timestamp: false, ExemptServices: []string{"live"}}, Avatar: avatarPolicy(), Cleanup: func(item plugin.Subscription) []plugin.KVSelector {
			return []plugin.KVSelector{{Prefix: "source:bilibili:follow:", Suffix: ":" + strings.TrimSpace(item.UID)}}
		},
		Account: &plugin.AccountAdapter{Validate: ValidateAccount, NewQRProvider: NewAccountQRProvider},
	}
}
