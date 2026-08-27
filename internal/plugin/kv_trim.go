package plugin

import (
	"context"
	"sort"
	"strings"
)

const maxSeenKeysPerSubscription = 300

// trimSeenKeys 裁剪所有订阅的 seen 去重记录。
func trimSeenKeys(ctx context.Context, actions HostActions, items []Subscription) {
	if actions == nil {
		return
	}
	for _, item := range items {
		trimSubscriptionSeenKeys(ctx, actions, item)
	}
}

func trimSubscriptionSeenKeys(ctx context.Context, actions HostActions, item Subscription) {
	prefix := "seen:" + strings.TrimSpace(item.ID) + ":"
	if prefix == "seen::" {
		return
	}
	result, err := actions.KVList(ctx, prefix)
	if err != nil {
		return
	}
	keys := make([]string, 0)
	for _, raw := range SliceValue(result["keys"]) {
		if key := StringScalar(raw); key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) <= maxSeenKeysPerSubscription {
		return
	}
	type entry struct {
		key string
		ts  int64
	}
	entries := make([]entry, 0, len(keys))
	for _, key := range keys {
		ts := int64(0)
		if value, err := actions.KVGet(ctx, key); err == nil {
			if stored, ok := ActionStoredValue(value); ok {
				ts = IntScalar(NestedValue(stored, "ts"))
			}
		}
		entries = append(entries, entry{key: key, ts: ts})
	}
	// 无 ts 的旧格式记录视为最旧，优先删除。
	sort.Slice(entries, func(i, j int) bool { return entries[i].ts < entries[j].ts })
	for _, entry := range entries[:len(entries)-maxSeenKeysPerSubscription] {
		_, _ = actions.KVDelete(ctx, entry.key)
	}
}

// cleanupRemovedSubscriptionKV 清理已删除订阅（或已移除服务）的 KV 记录：
// seen 去重、直播状态、零作品负缓存与解析缓存，避免订阅删除后残留累积。
// services 为本次移除的服务列表；为空表示删除整个订阅。
func (handler *Handler) cleanupRemovedSubscriptionKV(ctx context.Context, actions HostActions, current *Settings, removed *Subscription, services []string) {
	if actions == nil || removed == nil {
		return
	}
	stillExists := false
	for _, item := range current.Subscriptions {
		if item.ID == removed.ID {
			stillExists = true
			break
		}
	}
	prefix := "seen:" + removed.ID + ":"
	if stillExists {
		for _, service := range services {
			deleteKVPrefix(ctx, actions, prefix+service+":")
		}
		return
	}
	deleteKVPrefix(ctx, actions, prefix)
	definition := handler.byID[removed.Platform]
	if definition.Cleanup == nil {
		return
	}
	for _, selector := range definition.Cleanup(*removed) {
		result, err := actions.KVList(ctx, selector.Prefix)
		if err != nil {
			continue
		}
		for _, raw := range SliceValue(result["keys"]) {
			key := StringScalar(raw)
			if key != "" && strings.HasPrefix(key, selector.Prefix) && (selector.Suffix == "" || strings.HasSuffix(key, selector.Suffix)) {
				_, _ = actions.KVDelete(ctx, key)
			}
		}
	}

}

func deleteKVPrefix(ctx context.Context, actions HostActions, prefix string) {
	if actions == nil || strings.TrimSpace(prefix) == "" {
		return
	}
	result, err := actions.KVList(ctx, prefix)
	if err != nil {
		return
	}
	for _, raw := range SliceValue(result["keys"]) {
		if key := StringScalar(raw); key != "" {
			_, _ = actions.KVDelete(ctx, key)
		}
	}
}
