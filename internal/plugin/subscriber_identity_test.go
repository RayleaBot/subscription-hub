package plugin

import (
	"context"
	"errors"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func TestSubscriberIdentityRefreshUsesLiveGroupMemberInfoAndCaches(t *testing.T) {
	fake := newFakePluginActions()
	key := groupMemberKey("10001", "20002")
	fake.groupMembers[key] = rayleabot.ActionResult{
		"user_id":  "20002",
		"nickname": "实时昵称",
		"card":     "实时群名片",
		"title":    "实时头衔",
		"role":     "admin",
	}
	item := subscription{
		TargetType: "group",
		TargetID:   "10001",
		Subscribers: []subscriber{{
			ID: "20002", Nickname: "保存昵称", GroupNickname: "保存群名片",
			BaseRole: "member", Role: "member", RoleLabel: "群员",
		}},
	}
	cache := newSubscriberIdentityCache(context.Background())

	first, failed := cache.refresh(fake, item)
	if failed != 0 {
		t.Fatalf("failed refreshes = %d, want 0", failed)
	}
	second, failed := cache.refresh(fake, item)
	if failed != 0 {
		t.Fatalf("cached failed refreshes = %d, want 0", failed)
	}
	if len(fake.groupRequests) != 1 {
		t.Fatalf("group member requests = %d, want 1", len(fake.groupRequests))
	}
	for _, refreshed := range []subscriber{first.Subscribers[0], second.Subscribers[0]} {
		if refreshed.Nickname != "实时昵称" || refreshed.GroupNickname != "实时群名片" || refreshed.Title != "实时头衔" {
			t.Fatalf("refreshed identity = %#v", refreshed)
		}
		if refreshed.BaseRole != "admin" || refreshed.Role != "admin" || refreshed.RoleLabel != "管理员" {
			t.Fatalf("refreshed role = %#v", refreshed)
		}
	}
}

func TestSubscriberIdentityRefreshPreservesSuperAdminAndFallsBackOnFailure(t *testing.T) {
	fake := newFakePluginActions()
	cache := newSubscriberIdentityCache(context.Background())

	superKey := groupMemberKey("10001", "20002")
	fake.groupMembers[superKey] = rayleabot.ActionResult{"user_id": "20002", "role": "owner"}
	superItem, failed := cache.refresh(fake, subscription{
		TargetType: "group",
		TargetID:   "10001",
		Subscribers: []subscriber{{
			ID: "20002", Nickname: "订阅人", BaseRole: "member", Role: "super_admin", RoleLabel: "超级管理员",
		}},
	})
	if failed != 0 {
		t.Fatalf("failed refreshes = %d, want 0", failed)
	}
	if got := superItem.Subscribers[0]; got.BaseRole != "owner" || got.Role != "super_admin" || got.RoleLabel != "超级管理员" {
		t.Fatalf("super administrator identity = %#v", got)
	}

	failureKey := groupMemberKey("10001", "30003")
	fake.groupErrors[failureKey] = errors.New("adapter unavailable")
	stored := subscriber{ID: "30003", Nickname: "保存昵称", BaseRole: "admin", Role: "admin", RoleLabel: "管理员"}
	failures := []string{}
	refreshed := refreshSubscribersForDelivery(context.Background(), fake, subscription{
		ID: "fixture", TargetType: "group", TargetID: "10001", Subscribers: []subscriber{stored},
	}, cache, &failures)
	if len(failures) != 1 {
		t.Fatalf("failures = %#v, want one degraded warning", failures)
	}
	if got := refreshed.Subscribers[0]; got != stored {
		t.Fatalf("fallback identity = %#v, want %#v", got, stored)
	}
	if len(fake.logs) != 1 || fake.logs[0].Level != "warn" {
		t.Fatalf("logs = %#v, want one warning", fake.logs)
	}

	delete(fake.groupErrors, failureKey)
	fake.groupMembers[failureKey] = rayleabot.ActionResult{"user_id": "30003", "nickname": "恢复昵称", "role": "owner"}
	retried, failed := cache.refresh(fake, subscription{
		TargetType: "group", TargetID: "10001", Subscribers: []subscriber{stored},
	})
	if failed != 0 || retried.Subscribers[0].Nickname != "恢复昵称" || retried.Subscribers[0].Role != "owner" {
		t.Fatalf("failed refresh was cached instead of retried: failed=%d item=%#v", failed, retried)
	}
}
