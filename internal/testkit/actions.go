package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/httpaction"
)

type Actions struct {
	Location           *time.Location
	mu                 sync.Mutex
	Secrets            map[string]string
	HTTPRoutes         []HTTPRoute
	HTTPDefault        rayleabot.ActionResult
	HTTPFallback       func(httpaction.Request) (rayleabot.ActionResult, error, bool)
	HTTPResponses      []rayleabot.ActionResult
	HTTPErrors         []error
	HTTPRequests       []httpaction.Request
	KV                 map[string]any
	Renders            []rayleabot.RenderImageRequest
	ResourceRenders    []RenderRequest
	RenderErrors       []error
	Messages           []rayleabot.MessageSendRequest
	MessageErrors      []error
	Logs               []rayleabot.LoggerWriteRequest
	GroupMembers       map[string]rayleabot.ActionResult
	GroupErrors        map[string]error
	GroupRequests      []string
	BrowserLaunches    []rayleabot.BrowserLaunchRequest
	BrowserCloses      []string
	BrowserLaunchError error
	Config             map[string]any
	SchedulerRequests  []rayleabot.SchedulerCreateRequest
}

func NewActions() *Actions {
	return &Actions{
		Location:     time.FixedZone("Asia/Shanghai", 8*60*60),
		Secrets:      map[string]string{},
		KV:           map[string]any{},
		Config:       map[string]any{},
		GroupMembers: map[string]rayleabot.ActionResult{},
		GroupErrors:  map[string]error{},
	}
}

func (fake *Actions) TimeLocation() *time.Location { return fake.Location }

// SeedAccounts stores fixture profiles and credentials in plugin KV and secrets.
func (fake *Actions) SeedAccounts(platform string, payload rayleabot.ActionResult) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if payload == nil {
		return
	}
	items, _ := payload["accounts"].([]any)
	for index, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		accountID, _ := item["account_id"].(string)
		if accountID == "" {
			continue
		}
		label, _ := item["label"].(string)
		enabled := true
		if value, ok := item["enabled"].(bool); ok {
			enabled = value
		}
		record := map[string]any{
			"platform":         platform,
			"account_id":       accountID,
			"label":            label,
			"enabled":          enabled,
			"credential_state": "unknown",
			"updated_at":       fmt.Sprintf("2026-01-01T00:00:%02dZ", index),
		}
		if profile, ok := item["profile"].(map[string]any); ok {
			if value, ok := profile["uid"].(string); ok {
				record["uid"] = value
			}
			if value, ok := profile["nickname"].(string); ok {
				record["nickname"] = value
			}
			if value, ok := profile["avatar_url"].(string); ok {
				record["avatar_url"] = value
			}
		}
		fake.KV["account:"+platform+":"+accountID] = record
		if cookie, ok := item["cookie"].(map[string]any); ok {
			if value, ok := cookie["value"].(string); ok && value != "" {
				fake.Secrets["account."+platform+"."+accountID+".cookie"] = value
			}
		} else if value, ok := item["cookie"].(string); ok && value != "" {
			fake.Secrets["account."+platform+"."+accountID+".cookie"] = value
		}
	}
}

// AccountCredentialState returns the persisted credential state for one test
// account, if present.
func (fake *Actions) AccountCredentialState(platform, accountID string) (string, bool) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	raw, exists := fake.KV["account:"+platform+":"+accountID]
	if !exists {
		return "", false
	}
	if record, ok := raw.(map[string]any); ok {
		state, _ := record["credential_state"].(string)
		return state, true
	}
	value := reflect.ValueOf(raw)
	if value.Kind() == reflect.Struct {
		field := value.FieldByName("CredentialState")
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String(), true
		}
	}
	return "", false
}

func (fake *Actions) SecretRead(_ context.Context, key string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, exists := fake.Secrets[key]
	if !exists {
		return rayleabot.ActionResult{"key": key, "exists": false}, nil
	}
	return rayleabot.ActionResult{"key": key, "exists": true, "value": value}, nil
}

func (fake *Actions) SecretWrite(_ context.Context, values map[string]string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	changed := make([]string, 0, len(values))
	for key, value := range values {
		fake.Secrets[key] = value
		changed = append(changed, key)
	}
	return rayleabot.ActionResult{"changed_keys": changed}, nil
}

func (fake *Actions) SecretDelete(_ context.Context, keys []string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	changed := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, exists := fake.Secrets[key]; exists {
			delete(fake.Secrets, key)
			changed = append(changed, key)
		}
	}
	return rayleabot.ActionResult{"changed_keys": changed}, nil
}

func (fake *Actions) BrowserLaunch(_ context.Context, request rayleabot.BrowserLaunchRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.BrowserLaunches = append(fake.BrowserLaunches, request)
	if fake.BrowserLaunchError != nil {
		return nil, fake.BrowserLaunchError
	}
	return rayleabot.ActionResult{
		"session_id":   "browser-fixture",
		"debugger_url": "ws://127.0.0.1:9222/devtools/browser/fixture",
		"mode":         "headless",
	}, nil
}

func (fake *Actions) BrowserClose(_ context.Context, sessionID string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.BrowserCloses = append(fake.BrowserCloses, sessionID)
	return rayleabot.ActionResult{"closed": true}, nil
}

func (fake *Actions) Call(_ context.Context, action string, input any, output any) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	switch action {
	case "render.image":
		var request RenderRequest
		if err := decodeInput(input, &request); err != nil {
			return fmt.Errorf("unexpected render.image input %T", input)
		}
		fake.ResourceRenders = append(fake.ResourceRenders, request)
		fake.Renders = append(fake.Renders, rayleabot.RenderImageRequest{
			Template: request.Template, Data: request.Data, Theme: request.Theme, Output: request.Output, FallbackText: request.FallbackText,
		})
		if len(fake.RenderErrors) > 0 {
			err := fake.RenderErrors[0]
			fake.RenderErrors = fake.RenderErrors[1:]
			if err != nil {
				return err
			}
		}
		if result, ok := output.(*rayleabot.ActionResult); ok {
			*result = rayleabot.ActionResult{"image_path": "plugin-test.png"}
		}
		return nil
	default:
		return fmt.Errorf("unexpected generic local action %q", action)
	}
}

func decodeInput(input, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
