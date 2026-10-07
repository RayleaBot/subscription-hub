package plugin

import (
	"context"
	"errors"
	"testing"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func forwardNode(request map[string]any, index int) map[string]any {
	return MapValue(MapSliceValue(request["messages"])[index]["data"])
}

func forwardMedia(request map[string]any, index int) map[string]any {
	return MapSliceValue(forwardNode(request, index)["content"])[0]
}

func TestMediaAlwaysUsesForwardWithSubjectAndSender(t *testing.T) {
	for _, platform := range []string{"bilibili", "weibo", "douyin"} {
		for _, kind := range []string{"image", "video"} {
			for _, targetType := range []string{"group", "private"} {
				t.Run(platform+"/"+kind+"/"+targetType, func(t *testing.T) {
					actions := testkit.NewActions()
					err := sendPreparedResolverMedia(t.Context(), actions, platform,
						[]preparedResolverMedia{{Kind: kind, Path: "original-media"}}, ResolverMediaSettings{},
						resolverSendTarget{TargetType: targetType, TargetID: "10001", SubjectName: "内容作者", SenderID: "20002", SenderName: "链接发送人", SourceAdapter: "qq-main"})
					if err != nil || len(actions.Messages) != 0 || len(actions.ForwardRequests) != 1 {
						t.Fatalf("delivery = %#v, %#v, %v", actions.Messages, actions.ForwardRequests, err)
					}
					request := actions.ForwardRequests[0]
					if request["source"] != "内容作者" || request["target_type"] != targetType || request["target_id"] != "10001" || request["source_adapter"] != "qq-main" {
						t.Fatalf("forward metadata = %#v", request)
					}
					node := forwardNode(request, 0)
					media := forwardMedia(request, 0)
					if node["uin"] != "20002" || node["name"] != "链接发送人" || media["type"] != kind || NestedValue(media, "data", "file") != "original-media" {
						t.Fatalf("forward content = %#v", node)
					}
				})
			}
		}
	}
}

func TestForwardBatchesMixedMediaAndStopsAfterPartialFailure(t *testing.T) {
	media := []preparedResolverMedia{{Kind: "image", Path: "one"}, {Kind: "video", Path: "two"}, {Kind: "image", Path: "three"}}
	actions := testkit.NewActions()
	target := resolverSendTarget{TargetType: "group", TargetID: "10001", SubjectName: "作者", SenderID: "20002", SenderName: "订阅人"}
	if err := sendResolverForward(t.Context(), actions, "douyin", media, 2, target); err != nil {
		t.Fatal(err)
	}
	if len(actions.ForwardRequests) != 2 || len(MapSliceValue(actions.ForwardRequests[0]["messages"])) != 2 || len(MapSliceValue(actions.ForwardRequests[1]["messages"])) != 1 {
		t.Fatalf("batches = %#v", actions.ForwardRequests)
	}
	for index, request := range actions.ForwardRequests {
		if request["source"] != "作者" || forwardNode(request, 0)["uin"] != "20002" || NestedValue(forwardMedia(request, 0), "data", "file") != media[index*2].Path {
			t.Fatalf("batch metadata/order = %#v", request)
		}
	}
	actions.ForwardErrors = []error{nil, &rayleabot.ActionError{Code: "platform.rate_limited"}}
	err := sendResolverForward(t.Context(), actions, "douyin", media, 1, target)
	var partial *partialMediaSendError
	if !errors.As(err, &partial) || len(actions.ForwardRequests) != 4 {
		t.Fatalf("partial delivery = %v, requests=%d", err, len(actions.ForwardRequests))
	}
}

func TestMediaIdentityUsesActorAndFirstRecordedSubscriber(t *testing.T) {
	update := Update{"author": map[string]any{"name": "最新作者昵称"}}
	event := &rayleabot.EventContext{Bot: rayleabot.Bot{ID: "99999", Nickname: "机器人"}, Event: rayleabot.Event{
		SourceAdapter: "qq-main", Target: rayleabot.Target{Type: "group", ID: "10001"},
		Actor:   rayleabot.Actor{ID: "20002", Nickname: "发送人"},
		Payload: map[string]any{"onebot": map[string]any{"sender": map[string]any{"card": "群名片"}}},
	}}
	target := resolverMediaTarget(event, "weibo", update)
	if target.SubjectName != "最新作者昵称" || target.SenderID != "20002" || target.SenderName != "群名片" || target.SourceAdapter != "qq-main" {
		t.Fatalf("resolver identity = %#v", target)
	}
	item := Subscription{Name: "保存作者昵称", TargetType: "group", TargetID: "10001", Subscribers: []Subscriber{
		{}, {ID: "30003", Nickname: "订阅人", GroupNickname: "订阅人名片"}, {ID: "40004", Nickname: "第二人"},
	}}
	target = subscriptionMediaTarget(item, update)
	if target.SenderID != "30003" || target.SenderName != "订阅人名片" || target.SubjectName != "最新作者昵称" {
		t.Fatalf("subscription identity = %#v", target)
	}
	item.Subscribers = nil
	target = subscriptionMediaTarget(item, nil)
	actions := testkit.NewActions()
	if err := sendResolverForward(context.Background(), actions, "weibo", []preparedResolverMedia{{Kind: "image", Path: "original"}}, 1, target); err != nil {
		t.Fatal(err)
	}
	if _, exists := forwardNode(actions.ForwardRequests[0], 0)["uin"]; exists {
		t.Fatal("missing subscriber must let adapter use its own identity")
	}
	item.TargetType, item.TargetName = "private", "私聊用户"
	target = subscriptionMediaTarget(item, nil)
	if target.SenderID != "10001" || target.SenderName != "私聊用户" {
		t.Fatalf("legacy private subscription identity = %#v", target)
	}
}
