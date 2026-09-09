package plugin

import (
	"context"
	"net/url"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func NormalizePreviewURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		value = "https:" + value
	} else if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	parsed.Scheme, parsed.RawQuery, parsed.Fragment = "https", "", ""
	if len(parsed.Path) > 1 {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	return parsed.String()
}

func (handler *Handler) ParsePreviewInput(input string) (string, string) {
	input = strings.TrimSpace(input)
	lower := strings.ToLower(input)
	fields := strings.Fields(input)
	for _, platform := range handler.platforms {
		for _, alias := range platform.PreviewAliases {
			if len(fields) > 0 && strings.ToLower(fields[0]) == alias {
				rest := strings.Join(fields[1:], " ")
				service := platform.Services.Normalize(rest)
				if service == "" || service == "all" {
					service = platform.PreviewDefault
				}
				return platform.ID, service
			}
		}
		for _, prefix := range platform.PreviewPrefixes {
			if strings.HasPrefix(lower, prefix) && len(lower) > len(prefix) {
				service := platform.Services.Normalize(strings.TrimSpace(input[len(prefix):]))
				if service == "" || service == "all" {
					service = platform.PreviewDefault
				}
				return platform.ID, service
			}
		}
	}
	for _, platform := range handler.platforms {
		if service := FirstText(platform.PreviewInputs[input], platform.PreviewInputs[lower]); service != "" {
			return platform.ID, service
		}
	}
	for _, platform := range handler.platforms {
		if platform.PreviewDefault == "" {
			continue
		}
		service := platform.Services.Normalize(input)
		if service == "" || service == "all" {
			service = platform.PreviewDefault
		}
		return platform.ID, service
	}
	return "", ""
}

func (handler *Handler) previewSubscriptionCard(ctx context.Context, event *rayleabot.EventContext, input string) error {
	actions := handler.hostActions(event)
	var selected PreviewSession
	var update Update
	var platformID string
	for _, platform := range handler.platforms {
		if platform.PreviewDefault == "" {
			continue
		}
		session, ok := platform.NewSession(sourceBoundary(actions)).(PreviewSession)
		if !ok {
			continue
		}
		candidate, handled, err := session.Preview(ctx, strings.TrimSpace(input))
		if !handled {
			continue
		}
		if err != nil {
			return event.SendText(EnsureSentence(failureText(platform.ID, "preview", err.Error())))
		}
		platformID, selected, update = platform.ID, session, candidate
		break
	}
	if selected == nil {
		platform, service := handler.ParsePreviewInput(input)
		definition, ok := handler.byID[platform]
		if !ok {
			return event.SendText(ErrUnsupportedPreview.Error())
		}
		selected, ok = definition.NewSession(sourceBoundary(actions)).(PreviewSession)
		if !ok {
			return event.SendText(ErrUnsupportedPreview.Error())
		}
		platformID, update = platform, selected.Sample(service, handler.now().In(actions.TimeLocation()))
	}
	author := MapValue(update["author"])
	item := Subscription{
		ID:       "preview-" + platformID + "-" + NormalizedTargetType(event.Event.Target.Type) + "-" + FirstText(event.Event.Target.ID, "current"),
		Platform: platformID, UID: FirstText(author["uid"], update["uid"], "preview"),
		UniqueID: StringScalar(author["unique_id"]), Name: FirstText(author["name"], handler.platformName(platformID)+"预览"),
		TargetType: NormalizedTargetType(event.Event.Target.Type), TargetID: event.Event.Target.ID,
		Services: []string{"all"}, Subscribers: MergeSubscriber(nil, event), Enabled: true,
	}
	card := selected.UpdateCard(item, update)
	fields := PreviewRenderLogFields(update)
	fields["platform"], fields["stage"] = platformID, "render"
	imagePath, err := handler.renderCard(ctx, actions, card, nil, fields)
	if err != nil {
		return event.SendText(failureText(platformID, "render", PreviewCardFailureText(err)))
	}
	return event.Send(event.Event.Target.Type, event.Event.Target.ID, rayleabot.Image(imagePath))
}
