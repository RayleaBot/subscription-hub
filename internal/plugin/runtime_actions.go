package plugin

import (
	"context"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type SourceActions interface {
	BrowserLauncher
	TimeLocation() *time.Location
	HTTPRequest(context.Context, rayleabot.HTTPRequest) (rayleabot.ActionResult, error)
	SecretRead(context.Context, string) (rayleabot.ActionResult, error)
	KVGet(context.Context, string) (rayleabot.ActionResult, error)
	KVSet(context.Context, string, any) (rayleabot.ActionResult, error)
	KVDelete(context.Context, string) (rayleabot.ActionResult, error)
	KVList(context.Context, string) (rayleabot.ActionResult, error)
	LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error)
	BrowserClose(context.Context, string) (rayleabot.ActionResult, error)
}

type HostActions interface {
	SourceActions
	RenderImage(context.Context, rayleabot.RenderImageRequest) (rayleabot.ActionResult, error)
	MessageSend(context.Context, rayleabot.MessageSendRequest) (rayleabot.ActionResult, error)
	GroupMemberGet(context.Context, string, string) (rayleabot.ActionResult, error)
}

type RuntimeActions interface {
	HostActions
	SecretWrite(context.Context, map[string]string) (rayleabot.ActionResult, error)
	SecretDelete(context.Context, []string) (rayleabot.ActionResult, error)
	ConfigWrite(context.Context, map[string]any) (rayleabot.ActionResult, error)
	SchedulerCreate(context.Context, rayleabot.SchedulerCreateRequest) (rayleabot.ActionResult, error)
}

// sourceActions narrows the host surface available to source sessions to the
// SourceActions method set. Platform source code cannot reach generic host
// calls or privileged actions through this boundary.
type sourceActions struct {
	SourceActions
}

func sourceBoundary(actions SourceActions) SourceActions {
	return sourceActions{SourceActions: actions}
}

func (actions sourceActions) HTTPRequest(ctx context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return actions.SourceActions.HTTPRequest(ctx, request)
}

func (actions sourceActions) SecretRead(ctx context.Context, key string) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return actions.SourceActions.SecretRead(ctx, key)
}

// GenericLocalActionCaller exposes the SDK generic action call for host-side
// utility actions (render resources, OneBot file and forward) that have no
// dedicated typed helper. Source sessions never receive this interface.
type GenericLocalActionCaller interface {
	Call(context.Context, string, any, any) error
}
