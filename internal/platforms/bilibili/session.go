package bilibili

import (
	"context"
	"errors"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

type session struct{ actions plugin.SourceActions }

func (session *session) Resolve(ctx context.Context, request plugin.ResolveRequest) (plugin.Resolution, error) {
	users, err := resolveBilibiliUsersWithActions(ctx, session.actions, request.Query)
	if err != nil || len(users) == 0 {
		return plugin.Resolution{}, errors.New(friendlyBilibiliError(err))
	}
	result := plugin.Resolution{Candidates: sharedUsers(users)}
	if matched := matchBilibiliUserByQuery(users, request.Query); matched != nil {
		user := sharedUser(*matched)
		result.Matched = &user
	}
	if result.Matched == nil && request.Purpose == "subscribe" {
		result.Message = "没有找到昵称与「" + request.Query + "」完全一致的 Bilibili UP 主。可以参考下面的搜索结果，用更准确的昵称或 UID 重新订阅。"
	}
	return result, nil
}
func sharedUser(user bilibiliUser) plugin.User {
	return plugin.User{UID: user.UID, Name: user.Name, AvatarURL: user.AvatarURL, Profile: user}
}
func sharedUsers(users []bilibiliUser) []plugin.User {
	result := make([]plugin.User, 0, len(users))
	for _, user := range users {
		result = append(result, sharedUser(user))
	}
	return result
}
func nativeUsers(users []plugin.User) []bilibiliUser {
	result := make([]bilibiliUser, 0, len(users))
	for _, user := range users {
		if profile, ok := user.Profile.(bilibiliUser); ok {
			result = append(result, profile)
		}
	}
	return result
}
func (session *session) Search(ctx context.Context, query string) ([]plugin.User, error) {
	users, err := searchBilibiliWithActions(ctx, session.actions, query)
	if err != nil {
		return nil, errors.New(friendlyBilibiliError(err))
	}
	return sharedUsers(users), nil
}
func (session *session) SearchCard(ctx context.Context, query string, users []plugin.User, prefixes []string) plugin.CardRequest {
	profiles := prepareBilibiliSearchAvatars(ctx, session.actions, nativeUsers(users))
	return plugin.CardRequest{Template: bilibiliSearchResultsTemplate, Data: buildBilibiliSearchCardData(query, profiles, prefixes), Fallback: bilibiliSearchRenderErrorMessage}
}
func (session *session) UserCard(ctx context.Context, action string, item plugin.Subscription, resolved *plugin.User, services []string) plugin.CardRequest {
	user := bilibiliUser{UID: item.UID, Name: item.Name, AvatarURL: item.AvatarURL}
	if resolved != nil {
		if profile, ok := resolved.Profile.(bilibiliUser); ok {
			user = profile
		}
	}
	user.AvatarURL = inlineBilibiliCardAvatar(ctx, session.actions, plugin.FirstText(user.AvatarURL, item.AvatarURL))
	item.AvatarURL = ""
	return plugin.CardRequest{Template: bilibiliUserCardTemplate, Data: buildBilibiliUserCardData(action, item, user, services)}
}
func (session *session) Poll(ctx, stateCtx context.Context, items []plugin.Subscription, notBefore time.Time) plugin.PollResult {
	result := newBilibiliSource(session.actions).Poll(ctx, items)
	ready := map[string]bool{}
	if result.DynamicOK {
		for _, item := range items {
			ready[item.UID] = true
		}
	}
	for _, update := range result.Updates {
		update["platform"] = "bilibili"
	}
	return plugin.PollResult{Checked: result.Checked, Updates: result.Updates, Errors: result.Errors, ReadyUIDs: ready, Summary: map[string]any{"accounts": result.AccountCount, "dynamic_ok": result.DynamicOK, "live_ok": result.LiveOK}}
}
func (session *session) Prepare(ctx context.Context, update plugin.Update) (plugin.Update, error) {
	return prepareUpdate(ctx, session.actions, update)
}
func (session *session) UpdateCard(item plugin.Subscription, update plugin.Update) plugin.CardRequest {
	data := buildBilibiliRenderData(item, update)
	return plugin.CardRequest{Template: "bilibili-update", Data: data, Resources: prepareBilibiliUpdateResources(data), Fallback: buildBilibiliFallback(data), InlineAvatars: true}
}
func (session *session) Preview(ctx context.Context, input string) (plugin.Update, bool, error) {
	if ref := parseBilibiliPreviewURL(input); ref != nil {
		update, err := fetchBilibiliPreview(ctx, session.actions, ref)
		return update, true, err
	}
	if looksLikeBilibiliPreviewURL(input) {
		return nil, true, errors.New("暂不支持这个 Bilibili 链接。")
	}
	return nil, false, nil
}
func (session *session) Sample(service string, now time.Time) plugin.Update {
	return sampleBilibiliUpdate(service, now)
}
