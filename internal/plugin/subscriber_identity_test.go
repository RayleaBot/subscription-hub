package plugin

import (
	"context"
	"errors"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestSubscriberIdentityRefreshUsesLiveGroupMemberInfoAndCaches(t *testing.T) {
	fake := testkit.NewActions()
	key := GroupMemberKey("10001", "20002")
	fake.GroupMembers[key] = rayleabot.ActionResult{
		"user_id":  "20002",
		"nickname": "实时昵称",
		"card":     "实时群名片",
		"title":    "实时头衔",
		"role":     "admin",
	}
	item := Subscription{
		TargetType: "group",
		TargetID:   "10001",
		Subscribers: []Subscriber{{
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
	if len(fake.GroupRequests) != 1 {
		t.Fatalf("group member requests = %d, want 1", len(fake.GroupRequests))
	}
	for _, refreshed := range []Subscriber{first.Subscribers[0], second.Subscribers[0]} {
		if refreshed.Nickname != "实时昵称" || refreshed.GroupNickname != "实时群名片" || refreshed.Title != "实时头衔" {
			t.Fatalf("refreshed identity = %#v", refreshed)
		}
		if refreshed.BaseRole != "admin" || refreshed.Role != "admin" || refreshed.RoleLabel != "管理员" {
			t.Fatalf("refreshed role = %#v", refreshed)
		}
	}
}

func TestSubscriberIdentityRefreshPreservesSuperAdminAndFallsBackOnFailure(t *testing.T) {
	fake := testkit.NewActions()
	cache := newSubscriberIdentityCache(context.Background())

	superKey := GroupMemberKey("10001", "20002")
	fake.GroupMembers[superKey] = rayleabot.ActionResult{"user_id": "20002", "role": "owner"}
	superItem, failed := cache.refresh(fake, Subscription{
		TargetType: "group",
		TargetID:   "10001",
		Subscribers: []Subscriber{{
			ID: "20002", Nickname: "订阅人", BaseRole: "member", Role: "super_admin", RoleLabel: "超级管理员",
		}},
	})
	if failed != 0 {
		t.Fatalf("failed refreshes = %d, want 0", failed)
	}
	if got := superItem.Subscribers[0]; got.BaseRole != "owner" || got.Role != "super_admin" || got.RoleLabel != "超级管理员" {
		t.Fatalf("super administrator identity = %#v", got)
	}

	failureKey := GroupMemberKey("10001", "30003")
	fake.GroupErrors[failureKey] = errors.New("adapter unavailable")
	stored := Subscriber{ID: "30003", Nickname: "保存昵称", BaseRole: "admin", Role: "admin", RoleLabel: "管理员"}
	failures := []string{}
	refreshed := refreshSubscribersForDelivery(context.Background(), fake, Subscription{
		ID: "fixture", TargetType: "group", TargetID: "10001", Subscribers: []Subscriber{stored},
	}, cache, &failures)
	if len(failures) != 1 {
		t.Fatalf("failures = %#v, want one degraded warning", failures)
	}
	if got := refreshed.Subscribers[0]; got != stored {
		t.Fatalf("fallback identity = %#v, want %#v", got, stored)
	}
	if len(fake.Logs) != 1 || fake.Logs[0].Level != "warn" {
		t.Fatalf("logs = %#v, want one warning", fake.Logs)
	}

	delete(fake.GroupErrors, failureKey)
	fake.GroupMembers[failureKey] = rayleabot.ActionResult{"user_id": "30003", "nickname": "恢复昵称", "role": "owner"}
	retried, failed := cache.refresh(fake, Subscription{
		TargetType: "group", TargetID: "10001", Subscribers: []Subscriber{stored},
	})
	if failed != 0 || retried.Subscribers[0].Nickname != "恢复昵称" || retried.Subscribers[0].Role != "owner" {
		t.Fatalf("failed refresh was cached instead of retried: failed=%d item=%#v", failed, retried)
	}
}
