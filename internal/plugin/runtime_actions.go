package plugin

import (
	"context"
	"fmt"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type SourceActions interface {
	HTTPRequest(context.Context, rayleabot.HTTPRequest) (rayleabot.ActionResult, error)
	ThirdPartyAccountRead(context.Context, rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error)
	KVGet(context.Context, string) (rayleabot.ActionResult, error)
	KVSet(context.Context, string, any) (rayleabot.ActionResult, error)
	KVDelete(context.Context, string) (rayleabot.ActionResult, error)
	KVList(context.Context, string) (rayleabot.ActionResult, error)
	LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error)
}

type HostActions interface {
	SourceActions
	RenderImage(context.Context, rayleabot.RenderImageRequest) (rayleabot.ActionResult, error)
	MessageSend(context.Context, rayleabot.MessageSendRequest) (rayleabot.ActionResult, error)
	GroupMemberGet(context.Context, string, string) (rayleabot.ActionResult, error)
}

type RuntimeActions interface {
	HostActions
	ConfigRead(context.Context, ...string) (rayleabot.ActionResult, error)
	ConfigWrite(context.Context, map[string]any) (rayleabot.ActionResult, error)
	SchedulerCreate(context.Context, rayleabot.SchedulerCreateRequest) (rayleabot.ActionResult, error)
}

type sourceActions struct {
	SourceActions
	caller GenericLocalActionCaller
}

func sourceBoundary(actions SourceActions) SourceActions {
	caller, _ := actions.(GenericLocalActionCaller)
	return sourceActions{SourceActions: actions, caller: caller}
}

func (actions sourceActions) HTTPRequest(ctx context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return actions.SourceActions.HTTPRequest(ctx, request)
}

func (actions sourceActions) ThirdPartyAccountRead(ctx context.Context, request rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return actions.SourceActions.ThirdPartyAccountRead(ctx, request)
}

func (actions sourceActions) Call(ctx context.Context, action string, input, output any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actions.caller == nil || (action != "thirdparty.resolve" && action != "thirdparty.account.validate") {
		return fmt.Errorf("source action %q unavailable", action)
	}
	return actions.caller.Call(ctx, action, input, output)
}

type GenericLocalActionCaller interface {
	Call(context.Context, string, any, any) error
}

type AccountValidationRequest struct {
	Platform    string `json:"platform"`
	AccountID   string `json:"account_id"`
	Observation string `json:"observation"`
	HTTPStatus  int    `json:"http_status,omitempty"`
}
