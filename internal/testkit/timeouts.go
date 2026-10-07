package testkit

import (
	"context"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/httpaction"
)

type DeadlineActions struct {
	*Actions
	Deadline time.Time
}

func (actions *DeadlineActions) HTTPRequest(ctx context.Context, request httpaction.Request) (rayleabot.ActionResult, error) {
	if deadline, ok := ctx.Deadline(); ok {
		actions.mu.Lock()
		actions.Deadline = deadline
		actions.mu.Unlock()
	}
	return actions.Actions.HTTPRequest(ctx, request)
}
