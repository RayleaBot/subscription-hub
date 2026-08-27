package plugin

import (
	"context"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (handler *Handler) replySearch(ctx context.Context, event *rayleabot.EventContext, platform, query string) error {
	session := handler.byID[platform].NewSession(sourceBoundary(handler.hostActions(event)))
	search, ok := session.(SearchSession)
	if !ok {
		return event.SendText("此平台不支持用户搜索。")
	}
	users, err := search.Search(ctx, query)
	if err != nil {
		return event.SendText(failureText(platform, "search", err.Error()))
	}
	card := search.SearchCard(ctx, query, users, event.CommandPrefixes)
	return handler.replyCard(ctx, event, platform, card)
}

func (handler *Handler) replySubscriptionOutcome(ctx context.Context, event *rayleabot.EventContext, platform string, outcome SubscriptionOutcome) error {
	if outcome.Action == "candidates" {
		return handler.replySubscriptionCandidates(ctx, event, platform, outcome)
	}
	if !outcome.Changed || outcome.Item == nil {
		return event.SendText(outcome.Message)
	}
	session := outcome.session
	if session == nil {
		session = handler.byID[platform].NewSession(sourceBoundary(handler.hostActions(event)))
	}
	if cards, ok := session.(UserCardSession); ok {
		card := cards.UserCard(ctx, outcome.Action, *outcome.Item, outcome.User, outcome.Services)
		card.Fallback = outcome.Message
		return handler.replyCard(ctx, event, platform, card)
	}
	return event.SendText(outcome.Message)
}

func (handler *Handler) replyCard(ctx context.Context, event *rayleabot.EventContext, platform string, card CardRequest) error {
	imagePath, err := handler.renderReplyCard(ctx, handler.hostActions(event), card, map[string]any{"platform": platform, "stage": "render"})
	if err != nil {
		return event.SendText(card.Fallback)
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

// Candidate hints use a nonterminal message; the image or result ends the event.
func (handler *Handler) replySubscriptionCandidates(ctx context.Context, event *rayleabot.EventContext, platform string, outcome SubscriptionOutcome) error {
	actions := handler.hostActions(event)
	if _, err := actions.MessageSend(ctx, rayleabot.MessageSendRequest{
		TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Message: rayleabot.MessageOut{Segments: []rayleabot.Segment{rayleabot.Text(outcome.Message)}},
	}); err != nil {
		return event.SendText(outcome.Message)
	}
	search, ok := outcome.session.(SearchSession)
	if !ok || len(outcome.Candidates) == 0 {
		return event.Result(map[string]any{"handled": true, "card": false})
	}
	card := search.SearchCard(ctx, outcome.CandidatesQuery, outcome.Candidates, event.CommandPrefixes)
	imagePath, err := handler.renderReplyCard(ctx, actions, card, map[string]any{"platform": platform, "stage": "render"})
	if err != nil {
		return event.Result(map[string]any{"handled": true, "card": false})
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}

func (handler *Handler) renderCard(ctx context.Context, actions HostActions, card CardRequest, cache *AvatarCache, fields map[string]any) (string, error) {
	if card.InlineAvatars {
		handler.inlineUpdateAvatarsWithSharedCache(ctx, actions, card.Data, cache)
	}
	return RenderSubscriptionCardImageWithResources(ctx, actions, card.Template, card.Data, card.Resources, card.Fallback, fields)
}

func (handler *Handler) renderReplyCard(ctx context.Context, actions HostActions, card CardRequest, fields map[string]any) (string, error) {
	renderCtx, cancel := context.WithTimeout(ctx, CardRenderActionTimeout)
	defer cancel()
	return handler.renderCard(renderCtx, actions, card, nil, fields)
}
