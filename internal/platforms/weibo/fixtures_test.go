package weibo

import (
	"context"
	"testing"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func newHandler(t testing.TB, at ...time.Time) *plugin.Handler {
	t.Helper()
	options := plugin.Options{Platforms: []plugin.Platform{New()}, MediaTempRoot: t.TempDir()}
	if len(at) > 0 {
		options.Now = func() time.Time { return at[0] }
	}
	handler, err := plugin.NewHandler(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.Close)
	return handler
}
func checkAt(t testing.TB, ctx context.Context, actions plugin.HostActions, current plugin.Settings, now time.Time) map[string]any {
	t.Helper()
	return newHandler(t, now).Check(ctx, actions, current)
}
