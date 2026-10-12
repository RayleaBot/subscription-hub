package testkit

import (
	"context"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (fake *Actions) LoggerWrite(_ context.Context, request rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.Logs = append(fake.Logs, request)
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *Actions) RenderImage(_ context.Context, request rayleabot.RenderImageRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.Renders = append(fake.Renders, request)
	if len(fake.RenderErrors) > 0 {
		err := fake.RenderErrors[0]
		fake.RenderErrors = fake.RenderErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return rayleabot.ActionResult{"image_path": "plugin-test.png"}, nil
}

func (fake *Actions) MessageSend(_ context.Context, request rayleabot.MessageSendRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.Messages = append(fake.Messages, request)
	if len(fake.MessageErrors) > 0 {
		err := fake.MessageErrors[0]
		fake.MessageErrors = fake.MessageErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	return rayleabot.ActionResult{"message_id": "fixture-message"}, nil
}

func (fake *Actions) GroupMemberGet(_ context.Context, botID, groupID, userID string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	key := "onebot11\x00" + strings.TrimSpace(botID) + "\x00" + strings.TrimSpace(groupID) + "\x00" + strings.TrimSpace(userID)
	fake.GroupRequests = append(fake.GroupRequests, key)
	if err := fake.GroupErrors[key]; err != nil {
		return nil, err
	}
	if result := fake.GroupMembers[key]; result != nil {
		return result, nil
	}
	return rayleabot.ActionResult{"user_id": userID, "role": "member"}, nil
}

func (fake *Actions) SendTo(ctx context.Context, address rayleabot.Address, segments ...rayleabot.Segment) (rayleabot.ActionResult, error) {
	return fake.MessageSend(ctx, rayleabot.MessageSendRequest{SourceProtocol: address.SourceProtocol, BotID: address.BotID, TargetType: address.TargetType, TargetID: address.TargetID, Message: rayleabot.MessageOut{Segments: segments}})
}
