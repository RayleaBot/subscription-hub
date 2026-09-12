package testkit

import (
	"context"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type RenderResource struct {
	ID           string   `json:"id"`
	URL          string   `json:"url"`
	FallbackURLs []string `json:"fallback_urls,omitempty"`
	Referer      string   `json:"referer,omitempty"`
}

type RenderRequest struct {
	Template     string           `json:"template"`
	Data         map[string]any   `json:"data"`
	Theme        string           `json:"theme"`
	Output       string           `json:"output"`
	FallbackText string           `json:"fallback_text"`
	Resources    []RenderResource `json:"resources"`
}

func (fake *Actions) ConfigWrite(ctx context.Context, values map[string]any) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for key, value := range values {
		fake.Config[key] = value
	}
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *Actions) SchedulerCreate(ctx context.Context, request rayleabot.SchedulerCreateRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.SchedulerRequests = append(fake.SchedulerRequests, request)
	return rayleabot.ActionResult{"ok": true}, nil
}

func SubscriptionEvent(args ...string) *rayleabot.EventContext {
	return &rayleabot.EventContext{Config: map[string]any{}, Event: rayleabot.Event{
		Actor:   rayleabot.Actor{ID: "7", Nickname: "柒柒"},
		Target:  rayleabot.Target{Type: "group", ID: "100"},
		Payload: map[string]any{"args": args},
	}}
}
