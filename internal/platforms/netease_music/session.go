package netease_music

import (
	"context"
	"strings"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

type session struct{ actions plugin.SourceActions }

func (session *session) Resolve(ctx context.Context, request plugin.ResolveRequest) (plugin.Resolution, error) {
	uid := subjectIDFromInput(request.Query)
	if uid == "" {
		uid = plugin.SafeSubjectID(request.Query)
	}
	name := strings.TrimSpace(request.Query)
	if name == "" {
		name = uid
	}
	profile := map[string]any{"uid": uid, "name": name, "avatar_url": ""}
	user := plugin.User{UID: uid, Name: name, Profile: profile}
	result := plugin.Resolution{Candidates: []plugin.User{user}}
	if uid != "" {
		result.Matched = &user
	}
	return result, nil
}
