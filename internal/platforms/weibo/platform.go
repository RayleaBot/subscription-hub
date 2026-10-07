package weibo

import (
	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

var catalog = plugin.NewServiceCatalog(
	[]string{"post", "image", "video", "repost"},
	map[string]string{"all": "全部", "post": "微博", "image": "图片", "video": "视频", "repost": "转发"},
	map[string]string{"全部": "all", "全量": "all", "所有": "all", "微博": "post", "动态": "post", "文字": "post", "图片": "image", "图文": "image", "视频": "video", "转发": "repost"})

func New() plugin.Platform {
	return plugin.Platform{
		PreviewPrefixes: []string{"weibo", "微博"},
		ID:              "weibo", Name: "微博", SubjectLabel: "UID", ListTitle: "微博订阅列表", AllListTitle: "全部微博订阅列表",
		Commands: map[string]string{"订阅微博推送": "add", "取消微博推送": "remove", "微博搜索博主": "search", "微博订阅列表": "list", "全部微博订阅列表": "list_all"}, Services: catalog, ParseSubject: subjectIDFromInput,
		NewSession:     func(actions plugin.SourceActions) plugin.Session { return &session{actions: actions} },
		PreviewAliases: []string{"weibo", "微博"}, PreviewDefault: "post", PreviewInputs: map[string]string{"image": "image", "图片": "image", "文字": "post"}, Baseline: &plugin.BaselinePolicy{KeyPrefix: "source:weibo:feed:initialized:", Timestamp: true, ExemptServices: nil}, Avatar: avatarPolicy(), Cleanup: nil,
		Account: &plugin.AccountAdapter{Validate: ValidateAccount, NewQRProvider: NewAccountQRProvider},
	}
}
