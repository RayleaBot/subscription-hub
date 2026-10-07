package netease_music

import (
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"net/url"
	"strings"
)

var catalog = plugin.NewServiceCatalog(
	[]string{"song", "album", "playlist", "artist"},
	map[string]string{"all": "全部", "song": "歌曲", "album": "专辑", "playlist": "歌单", "artist": "音乐人"},
	map[string]string{"全部": "all", "全量": "all", "所有": "all", "歌曲": "song", "音乐": "song", "单曲": "song", "专辑": "album", "歌单": "playlist", "音乐人": "artist", "歌手": "artist"})

func New() plugin.Platform {
	return plugin.Platform{
		ID: "netease_music", Name: "网易云音乐", SubjectLabel: "ID", ListTitle: "网易云音乐订阅列表", AllListTitle: "全部网易云音乐订阅列表",
		Commands: map[string]string{"订阅网易云音乐推送": "add", "取消网易云音乐推送": "remove", "网易云音乐订阅列表": "list", "全部网易云音乐订阅列表": "list_all"}, Services: catalog, ParseSubject: subjectIDFromInput,
		NewSession:     func(actions plugin.SourceActions) plugin.Session { return &session{actions: actions} },
		PreviewAliases: nil, PreviewDefault: "", PreviewInputs: nil, Baseline: nil, Avatar: plugin.AvatarPolicy{Validate: func(parsed *url.URL) (string, bool) {
			return "https://music.163.com/", plugin.HostMatches(strings.ToLower(parsed.Hostname()), "music.126.net", "music.163.com")
		}}, Cleanup: nil,
		Account: &plugin.AccountAdapter{Validate: ValidateAccount, NewQRProvider: NewAccountQRProvider},
	}
}
