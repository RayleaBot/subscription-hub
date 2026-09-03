package plugin

import (
	"context"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const interactiveReplyTimeout = 50 * time.Second

func Run(ctx context.Context, platforms ...Platform) error {
	handler, err := NewHandler(Options{Platforms: platforms})
	if err != nil {
		return err
	}
	return rayleabot.Run(ctx, rayleabot.Options{}, handler)
}

func (handler *Handler) hostActions(event *rayleabot.EventContext) RuntimeActions {
	if handler.actions != nil {
		return handler.actions
	}
	return event.Actions()
}

func (handler *Handler) Handle(ctx context.Context, event *rayleabot.EventContext) error {
	switch event.Event.EventType {
	case "plugin.started":
		return event.Result(map[string]any{"handled": true, "scheduler_registered": handler.ensureScheduler(ctx, event)})
	case "config.changed":
		_, _ = handler.loadSettings(ctx, event)
		return event.Result(map[string]any{"handled": true, "reloaded": true})
	case "scheduler.trigger":
		action := FirstText(event.Event.Payload["action"], NestedValue(event.Event.Payload, "payload", "action"))
		if action == "flush_deferred_media" {
			ctx, cancel := context.WithTimeout(ctx, interactiveReplyTimeout)
			defer cancel()
			handler.flushDeferredMedia(ctx, event)
			return event.Result(map[string]any{"handled": true, "media_flushed": true})
		}
		if action != "" && action != "check_subscriptions" {
			return event.Result(map[string]any{"handled": false})
		}
		if delay := handler.jitter(); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		ctx, cancel := context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
		current, err := handler.loadSettings(ctx, event)
		if err != nil {
			return err
		}
		result := handler.Check(ctx, handler.hostActions(event), current)
		logSubscriptionCheck(ctx, handler.hostActions(event), result)
		return event.Result(result)
	case "management.action":
		return handler.handleManagementAction(ctx, event)
	default:
		return handler.handleCommand(ctx, event)
	}
}

func (handler *Handler) handleCommand(ctx context.Context, event *rayleabot.EventContext) error {
	platform, operation := handler.CommandOperation(event.Event.Command())
	if operation == "" {
		return handler.handleResolverMessage(ctx, event)
	}
	if InteractiveCommandOperation(operation) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, interactiveReplyTimeout)
		defer cancel()
	}
	query := strings.Join(event.Event.Args(), " ")
	if operation == "search" {
		return handler.replySearch(ctx, event, platform, query)
	}
	if operation == "preview" {
		return handler.previewSubscriptionCard(ctx, event, query)
	}
	current, err := handler.loadSettings(ctx, event)
	if err != nil {
		return err
	}
	switch operation {
	case "resolver_help":
		return handler.replyResolverHelp(ctx, event, current)
	case "status":
		return event.SendText(handler.FormatStatus(current))
	case "resolver_enable_bilibili", "resolver_disable_bilibili", "resolver_enable_weibo", "resolver_disable_weibo", "resolver_enable_douyin", "resolver_disable_douyin":
		return handler.toggleResolverForCurrentTarget(ctx, event, &current, operation)
	case "add", "remove":
		var outcome SubscriptionOutcome
		if operation == "add" {
			outcome = handler.AddSubscription(ctx, handler.hostActions(event), &current, event, platform)
		} else {
			outcome = handler.RemoveSubscription(&current, event, platform)
		}
		if outcome.Changed {
			if operation == "remove" {
				handler.cleanupRemovedSubscriptionKV(ctx, handler.hostActions(event), &current, outcome.Item, outcome.Services)
			}
			if err := handler.saveSettings(ctx, event, current); err != nil {
				return err
			}
		}
		return handler.replySubscriptionOutcome(ctx, event, platform, outcome)
	case "list", "list_all":
		return event.SendText(handler.FormatSubscriptions(current, event, platform, operation == "list_all"))
	case "check":
		return event.SendText(subscriptionCheckSummary(handler.Check(ctx, handler.hostActions(event), current)))
	}
	return event.Result(map[string]any{"handled": false})
}

func InteractiveCommandOperation(operation string) bool {
	switch operation {
	case "add", "remove", "search", "preview", "check":
		return true
	}
	return false
}

func stringValue(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
