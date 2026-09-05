package douyin

import (
	"context"
	"errors"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

type session struct{ actions plugin.SourceActions }

func (session *session) Resolve(ctx context.Context, request plugin.ResolveRequest) (plugin.Resolution, error) {

	if request.Purpose == "subscribe" || request.Purpose == "management" {
		if known := douyinSubscriptionSourceFromInput(&plugin.Settings{Subscriptions: request.Subscriptions}, request.Query); known != nil {
			user := sharedUser(*known)
			return plugin.Resolution{Matched: &user, Candidates: []plugin.User{user}}, nil
		}
	}
	users, err := resolveDouyinUsersWithActions(ctx, session.actions, request.Query)
	if err != nil || len(users) == 0 {
		return plugin.Resolution{}, errors.New(friendlyDouyinError(err))
	}
	result := plugin.Resolution{Candidates: sharedUsers(users)}
	if matched := matchDouyinUserByQuery(users, request.Query); matched != nil {
		user := sharedUser(*matched)
		result.Matched = &user
	}
	if result.Matched == nil && request.Purpose == "subscribe" {
		result.Message = "没有找到昵称或来源标识与「" + request.Query + "」完全一致的抖音用户。可以参考下面的搜索结果，或直接使用用户主页链接或完整 sec_uid 订阅。"
	}
	return result, nil
}
func sharedUser(user douyinUser) plugin.User {
	return plugin.User{UID: user.UID, Name: user.Name, AvatarURL: user.AvatarURL, UniqueID: user.UniqueID, Profile: user}
}
func sharedUsers(users []douyinUser) []plugin.User {
	result := make([]plugin.User, 0, len(users))
	for _, user := range users {
		result = append(result, sharedUser(user))
	}
	return result
}
func nativeUsers(users []plugin.User) []douyinUser {
	result := make([]douyinUser, 0, len(users))
	for _, user := range users {
		if profile, ok := user.Profile.(douyinUser); ok {
			result = append(result, profile)
		}
	}
	return result
}
func (session *session) Search(ctx context.Context, query string) ([]plugin.User, error) {
	users, err := searchDouyinWithActions(ctx, session.actions, query)
	if err != nil {
		return nil, errors.New(friendlyDouyinError(err))
	}
	return sharedUsers(users), nil
}
func (session *session) SearchCard(ctx context.Context, query string, users []plugin.User, prefixes []string) plugin.CardRequest {
	profiles := prepareDouyinSearchAvatars(ctx, session.actions, nativeUsers(users))
	return plugin.CardRequest{Template: douyinSearchResultsTemplate, Data: buildDouyinSearchCardData(query, profiles, prefixes), Fallback: douyinSearchRenderErrorMessage}
}
func (session *session) UserCard(ctx context.Context, action string, item plugin.Subscription, resolved *plugin.User, services []string) plugin.CardRequest {
	user := douyinUser{UID: item.UID, Name: item.Name, AvatarURL: item.AvatarURL}
	if resolved != nil {
		if profile, ok := resolved.Profile.(douyinUser); ok {
			user = profile
		}
	}
	user.AvatarURL = inlineDouyinCardAvatar(ctx, session.actions, plugin.FirstText(user.AvatarURL, item.AvatarURL))
	return plugin.CardRequest{Template: douyinUserCardTemplate, Data: buildDouyinUserCardData(action, item, user, services)}
}
func (session *session) Poll(ctx, stateCtx context.Context, items []plugin.Subscription, notBefore time.Time) plugin.PollResult {
	result := newDouyinSource(session.actions).PollSinceWithStateContext(ctx, stateCtx, items, notBefore)
	return plugin.PollResult{Checked: result.Checked, Updates: result.Updates, Errors: result.Errors, PauseReasons: result.PauseReasons, FailureKinds: result.FailureKinds, ReadyUIDs: result.ReadyUIDs, Summary: map[string]any{"accounts": result.AccountCount, "feed_ok": result.FeedOK, "paused": result.Paused}}
}
func (session *session) Prepare(ctx context.Context, update plugin.Update) (plugin.Update, error) {
	return plugin.CloneJSONMap(update), nil
}
func (session *session) UpdateCard(item plugin.Subscription, update plugin.Update) plugin.CardRequest {
	data := buildDouyinRenderData(item, update)
	return plugin.CardRequest{Template: "douyin-update", Data: data, Resources: prepareDouyinUpdateResources(data), Fallback: buildDouyinFallback(data), InlineAvatars: true}
}
func (session *session) Preview(ctx context.Context, input string) (plugin.Update, bool, error) {
	if ref := parseDouyinPreviewURL(input); ref != nil {
		update, err := fetchDouyinPreview(ctx, session.actions, ref)
		return update, true, err
	}
	if looksLikeDouyinPreviewURL(input) {
		return nil, true, errors.New("暂不支持这个 抖音 链接。")
	}
	return nil, false, nil
}
func (session *session) Sample(service string, now time.Time) plugin.Update {
	return sampleDouyinUpdate(service, now)
}
