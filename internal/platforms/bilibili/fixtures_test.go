package bilibili

import (
	"context"
	"testing"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func newHandler(t testing.TB, at ...time.Time) *plugin.Handler {
	t.Helper()
	options := plugin.Options{Platforms: []plugin.Platform{New()}}
	if len(at) > 0 {
		options.Now = func() time.Time { return at[0] }
	}
	handler, err := plugin.NewHandler(options)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
func checkAt(t testing.TB, ctx context.Context, actions plugin.HostActions, current plugin.Settings, now time.Time) map[string]any {
	t.Helper()
	return newHandler(t, now).Check(ctx, actions, current)
}
func newActions() *testkit.Actions {
	actions := testkit.NewActions()
	actions.HTTPDefault = testkit.HTTPJSON(200, map[string]any{"code": 0, "data": map[string]any{}})
	return actions
}

func dynamicSourceKey(item plugin.Subscription) string {
	return "source:bilibili:dynamic:initialized:" + item.ID
}
