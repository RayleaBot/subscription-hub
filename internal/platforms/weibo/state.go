package weibo

import (
	"context"
	"time"

	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

func weiboFeedSourceKey(item plugin.Subscription) string {
	return "source:weibo:feed:initialized:" + item.ID
}

func weiboFeedInitialized(ctx context.Context, actions plugin.SourceActions, item plugin.Subscription) bool {
	initialized, _ := weiboFeedState(ctx, actions, item)
	return initialized
}

func weiboFeedState(ctx context.Context, actions plugin.SourceActions, item plugin.Subscription) (bool, time.Time) {
	initialized, baseline, _ := plugin.ReadBaseline(ctx, actions, weiboFeedSourceKey(item))
	return initialized, baseline
}
