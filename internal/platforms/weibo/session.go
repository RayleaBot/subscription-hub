package weibo

import (
	"context"
	"errors"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

type session struct {
	actions  plugin.SourceActions
	longText *weiboLongTextResolver
}

func (session *session) Resolve(ctx context.Context, request plugin.ResolveRequest) (plugin.Resolution, error) {
	users, err := resolveWeiboUsersWithActions(ctx, session.actions, request.Query)

	if (err != nil || len(users) == 0) && request.Purpose == "subscribe" {
		if uid := weiboUIDFromInput(request.Query); uid != "" {
			user := sharedUser(weiboUser{UID: uid, Name: uid})
			return plugin.Resolution{Matched: &user, Candidates: []plugin.User{user}}, nil
		}
	}
	if err != nil || len(users) == 0 {
		return plugin.Resolution{}, errors.New(friendlyWeiboError(err))
	}
	result := plugin.Resolution{Candidates: sharedUsers(users)}
	if matched := matchWeiboUserByQuery(users, request.Query); matched != nil {
		user := sharedUser(*matched)
		result.Matched = &user
	}
	if result.Matched == nil && request.Purpose == "subscribe" {
		result.Message = "没有找到昵称与「" + request.Query + "」完全一致的微博博主。可以参考下面的搜索结果，用更准确的昵称或 UID 重新订阅。"
	}
	return result, nil
}
func sharedUser(user weiboUser) plugin.User {
	return plugin.User{UID: user.UID, Name: user.Name, AvatarURL: user.AvatarURL, Profile: user}
}
func sharedUsers(users []weiboUser) []plugin.User {
	result := make([]plugin.User, 0, len(users))
	for _, user := range users {
		result = append(result, sharedUser(user))
	}
	return result
}
func nativeUsers(users []plugin.User) []weiboUser {
	result := make([]weiboUser, 0, len(users))
	for _, user := range users {
		if profile, ok := user.Profile.(weiboUser); ok {
			result = append(result, profile)
		}
	}
	return result
}
func (session *session) Search(ctx context.Context, query string) ([]plugin.User, error) {
	users, err := searchWeiboWithActions(ctx, session.actions, query)
	if err != nil {
		return nil, errors.New(friendlyWeiboError(err))
	}
	return sharedUsers(users), nil
}
func (session *session) SearchCard(ctx context.Context, query string, users []plugin.User, prefixes []string) plugin.CardRequest {
	profiles := prepareWeiboSearchAvatars(ctx, session.actions, nativeUsers(users))
	return plugin.CardRequest{Template: weiboSearchResultsTemplate, Data: buildWeiboSearchCardData(query, profiles, prefixes), Fallback: weiboSearchRenderErrorMessage}
}
func (session *session) UserCard(ctx context.Context, action string, item plugin.Subscription, resolved *plugin.User, services []string) plugin.CardRequest {
	user := weiboUser{UID: item.UID, Name: item.Name, AvatarURL: item.AvatarURL}
	if resolved != nil {
		if profile, ok := resolved.Profile.(weiboUser); ok {
			user = profile
		}
	}
	user.AvatarURL = inlineWeiboCardAvatar(ctx, session.actions, plugin.FirstText(user.AvatarURL, item.AvatarURL))
	return plugin.CardRequest{Template: weiboUserCardTemplate, Data: buildWeiboUserCardData(action, item, user, services)}
}
func (session *session) Poll(ctx, stateCtx context.Context, items []plugin.Subscription, notBefore time.Time) plugin.PollResult {
	result := newWeiboSource(session.actions).PollSinceWithStateContext(ctx, stateCtx, items, notBefore)
	session.longText = newWeiboLongTextResolver(session.actions, result.Accounts)
	return plugin.PollResult{Checked: result.Checked, Updates: result.Updates, Errors: result.Errors, ReadyUIDs: result.ReadyUIDs, Summary: map[string]any{"accounts": result.AccountCount, "feed_ok": result.FeedOK}}
}
func (session *session) Prepare(ctx context.Context, update plugin.Update) (plugin.Update, error) {
	prepared, incomplete := session.longText.Prepare(ctx, update)
	if incomplete {
		return prepared, errors.New(weiboLongTextFailureText)
	}
	return prepared, nil
}
func (session *session) UpdateCard(item plugin.Subscription, update plugin.Update) plugin.CardRequest {
	data := buildWeiboRenderData(item, update)
	return plugin.CardRequest{Template: "weibo-update", Data: data, Resources: prepareWeiboUpdateResources(data), Fallback: buildWeiboFallback(data), InlineAvatars: true}
}
func (session *session) Preview(ctx context.Context, input string) (plugin.Update, bool, error) {
	if ref := parseWeiboPreviewURL(input); ref != nil {
		update, err := fetchWeiboPreview(ctx, session.actions, ref)
		return update, true, err
	}
	if isWeiboShortURL(input) {
		return nil, true, errors.New("微博短链展开失败，请发送完整微博链接。")
	}
	if looksLikeWeiboPreviewURL(input) {
		return nil, true, errors.New("暂不支持这个 微博 链接。")
	}
	return nil, false, nil
}
func (session *session) Sample(service string, now time.Time) plugin.Update {
	return sampleWeiboUpdate(service, now)
}
