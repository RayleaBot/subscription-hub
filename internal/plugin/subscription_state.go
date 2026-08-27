package plugin

import (
	"context"
	"fmt"
	"time"
)

func SubscriptionUpdateKey(item Subscription, update map[string]any) string {
	return fmt.Sprintf("seen:%s:%s:%s", item.ID, StringScalar(update["service"]), StringScalar(update["id"]))
}

func updateAtOrBeforeBaseline(update map[string]any, baselineAt time.Time) bool {
	if baselineAt.IsZero() {
		return false
	}
	publishedAt := IntScalar(update["pub_ts"])
	return publishedAt <= 0 || !time.Unix(publishedAt, 0).After(baselineAt)
}

func updateSeen(ctx context.Context, actions SourceActions, item Subscription, update map[string]any) (bool, error) {
	result, err := actions.KVGet(ctx, SubscriptionUpdateKey(item, update))
	_, exists := ActionStoredValue(result)
	return exists, err
}

func markUpdateSeen(ctx context.Context, actions SourceActions, item Subscription, update map[string]any, now time.Time) error {
	// 值携带写入时间戳，供 seen 裁剪按新旧排序。
	_, err := actions.KVSet(ctx, SubscriptionUpdateKey(item, update), map[string]any{"ts": now.Unix()})
	return err
}
