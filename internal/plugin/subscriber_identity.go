package plugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const subscriberIdentityRefreshTimeout = 6 * time.Second

const subscriberIdentityRefreshConcurrency = 4

type subscriberIdentityCacheEntry struct {
	ready chan struct{}
	value Subscriber
	err   error
}

type subscriberIdentityCache struct {
	parent    context.Context
	mu        sync.Mutex
	entries   map[string]*subscriberIdentityCacheEntry
	semaphore chan struct{}
}

func newSubscriberIdentityCache(parent context.Context) *subscriberIdentityCache {
	if parent == nil {
		parent = context.Background()
	}
	return &subscriberIdentityCache{
		parent:    parent,
		entries:   map[string]*subscriberIdentityCacheEntry{},
		semaphore: make(chan struct{}, subscriberIdentityRefreshConcurrency),
	}
}

func GroupMemberKey(groupID, userID string) string {
	return strings.TrimSpace(groupID) + "\x00" + strings.TrimSpace(userID)
}

func (cache *subscriberIdentityCache) resolve(ctx context.Context, actions HostActions, groupID string, current Subscriber) (Subscriber, error) {
	key := GroupMemberKey(groupID, current.ID)
	cache.mu.Lock()
	entry, exists := cache.entries[key]
	if !exists {
		entry = &subscriberIdentityCacheEntry{ready: make(chan struct{})}
		cache.entries[key] = entry
	}
	cache.mu.Unlock()

	if exists {
		select {
		case <-entry.ready:
			return entry.value, entry.err
		case <-ctx.Done():
			return current, ctx.Err()
		}
	}

	select {
	case cache.semaphore <- struct{}{}:
		defer func() { <-cache.semaphore }()
	case <-ctx.Done():
		cache.finish(key, entry, current, ctx.Err())
		return current, ctx.Err()
	}

	result, err := actions.GroupMemberGet(ctx, strings.TrimSpace(groupID), strings.TrimSpace(current.ID))
	if err == nil {
		current, err = subscriberFromGroupMember(current, result)
	}
	cache.finish(key, entry, current, err)
	return current, err
}

func (cache *subscriberIdentityCache) finish(key string, entry *subscriberIdentityCacheEntry, value Subscriber, err error) {
	cache.mu.Lock()
	entry.value = value
	entry.err = err
	if err != nil && cache.entries[key] == entry {
		delete(cache.entries, key)
	}
	close(entry.ready)
	cache.mu.Unlock()
}

func subscriberFromGroupMember(current Subscriber, result rayleabot.ActionResult) (Subscriber, error) {
	current.ID = strings.TrimSpace(current.ID)
	if current.ID == "" {
		return current, fmt.Errorf("subscriber id is empty")
	}
	if returnedID := StringScalar(result["user_id"]); returnedID != "" && returnedID != current.ID {
		return current, fmt.Errorf("group member id mismatch")
	}
	baseRole := strongestSubscriberBaseRole(StringScalar(result["role"]))
	if baseRole == "" {
		return current, fmt.Errorf("group member role is unavailable")
	}

	current.Nickname = FirstText(result["nickname"], current.Nickname, current.ID)
	current.GroupNickname = strings.TrimSpace(StringScalar(result["card"]))
	current.Title = strings.TrimSpace(StringScalar(result["title"]))
	current.BaseRole = baseRole
	current.Role = strings.ToLower(strings.TrimSpace(current.Role))
	if current.Role != "super_admin" {
		current.Role = baseRole
	}
	current.RoleLabel = subscriberRoleLabel(current.Role)
	current.AvatarURL = FirstText(qqAvatarURL(current.ID), current.AvatarURL)
	return current, nil
}

type subscriberIdentityResult struct {
	index int
	value Subscriber
	err   error
}

func (cache *subscriberIdentityCache) refresh(actions HostActions, item Subscription) (Subscription, int) {
	if cache == nil || NormalizedTargetType(item.TargetType) != "group" || len(item.Subscribers) == 0 {
		return item, 0
	}

	item.Subscribers = append([]Subscriber(nil), item.Subscribers...)
	refreshCtx, cancel := context.WithTimeout(cache.parent, subscriberIdentityRefreshTimeout)
	defer cancel()
	results := make(chan subscriberIdentityResult, len(item.Subscribers))
	var group sync.WaitGroup
	for index, current := range item.Subscribers {
		group.Add(1)
		go func(index int, current Subscriber) {
			defer group.Done()
			value, err := cache.resolve(refreshCtx, actions, item.TargetID, current)
			results <- subscriberIdentityResult{index: index, value: value, err: err}
		}(index, current)
	}
	group.Wait()
	close(results)

	failed := 0
	for result := range results {
		if result.err != nil {
			failed++
			continue
		}
		item.Subscribers[result.index] = result.value
	}
	return item, failed
}

func refreshSubscribersForDelivery(ctx context.Context, actions HostActions, item Subscription, cache *subscriberIdentityCache, failures *[]string) Subscription {
	refreshed, failed := cache.refresh(actions, item)
	if failed == 0 {
		return refreshed
	}
	*failures = append(*failures, "订阅人身份实时刷新失败，已使用保存信息。")
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   "warn",
		Message: "订阅人身份实时刷新不完整",
		Fields: map[string]any{
			"subscription_id":  item.ID,
			"target_type":      item.TargetType,
			"target_id":        item.TargetID,
			"subscriber_count": len(item.Subscribers),
			"refreshed":        len(item.Subscribers) - failed,
			"failed":           failed,
		},
	})
	return refreshed
}
