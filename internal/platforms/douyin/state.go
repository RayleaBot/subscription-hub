package douyin

import (
	"context"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func douyinFeedSourceKey(item plugin.Subscription) string {
	return "source:douyin:feed:initialized:" + item.ID
}

func douyinFeedInitialized(ctx context.Context, actions plugin.SourceActions, item plugin.Subscription) bool {
	initialized, _ := douyinFeedState(ctx, actions, item)
	return initialized
}

func douyinFeedState(ctx context.Context, actions plugin.SourceActions, item plugin.Subscription) (bool, time.Time) {
	initialized, baseline, _ := plugin.ReadBaseline(ctx, actions, douyinFeedSourceKey(item))
	return initialized, baseline
}
