package plugin

import (
	"context"
	"sort"
	"strings"
)

// maxSeenKeysPerSubscription 是单个订阅投递去重记录（seen: 前缀）的保留上限。
// 超出后按写入时间删除最旧的记录，防止 KV 无限增长。
const maxSeenKeysPerSubscription = 300

// trimSeenKeys 裁剪所有订阅的 seen 去重记录。
func trimSeenKeys(ctx context.Context, actions pluginActions, items []subscription) {
	if actions == nil {
		return
	}
	for _, item := range items {
		trimSubscriptionSeenKeys(ctx, actions, item)
	}
}

func trimSubscriptionSeenKeys(ctx context.Context, actions pluginActions, item subscription) {
	prefix := "seen:" + strings.TrimSpace(item.ID) + ":"
	if prefix == "seen::" {
		return
	}
	result, err := actions.KVList(ctx, prefix)
	if err != nil {
		return
	}
	keys := make([]string, 0)
	for _, raw := range sliceValue(result["keys"]) {
		if key := stringScalar(raw); key != "" {
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
			if stored, ok := actionStoredValue(value); ok {
				ts = intScalar(nestedValue(stored, "ts"))
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
func cleanupRemovedSubscriptionKV(ctx context.Context, actions pluginActions, current *settings, removed *subscription, services []string) {
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
	deleteKVPrefix(ctx, actions, "source:douyin:resolved_uid:"+strings.TrimSpace(removed.UID))
	deleteKVPrefix(ctx, actions, "source:douyin:live:"+strings.TrimSpace(removed.UID))
	deleteKVPrefix(ctx, actions, "source:douyin:zero_posts:"+strings.TrimSpace(removed.UID))
	// B 站自动关注状态按账号:uid 记录，删除订阅时一并清理。
	if removed.Platform == "bilibili" {
		if result, err := actions.KVList(ctx, "source:bilibili:follow:"); err == nil {
			for _, raw := range sliceValue(result["keys"]) {
				key := stringScalar(raw)
				if key != "" && strings.HasSuffix(key, ":"+strings.TrimSpace(removed.UID)) {
					_, _ = actions.KVDelete(ctx, key)
				}
			}
		}
	}
}

func deleteKVPrefix(ctx context.Context, actions pluginActions, prefix string) {
	if actions == nil || strings.TrimSpace(prefix) == "" {
		return
	}
	result, err := actions.KVList(ctx, prefix)
	if err != nil {
		return
	}
	for _, raw := range sliceValue(result["keys"]) {
		if key := stringScalar(raw); key != "" {
			_, _ = actions.KVDelete(ctx, key)
		}
	}
}
