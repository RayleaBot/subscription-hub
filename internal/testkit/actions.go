package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type Actions struct {
	Location           *time.Location
	mu                 sync.Mutex
	Accounts           rayleabot.ActionResult
	HTTPRoutes         []HTTPRoute
	HTTPDefault        rayleabot.ActionResult
	HTTPFallback       func(rayleabot.HTTPRequest) (rayleabot.ActionResult, error, bool)
	HTTPResponses      []rayleabot.ActionResult
	HTTPErrors         []error
	HTTPRequests       []rayleabot.HTTPRequest
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
	AccountValidations []AccountValidationRequest
	ResolveRequests    []ResolveRequest
	ResolveResults     []rayleabot.ActionResult
	Config             map[string]any
	SchedulerRequests  []rayleabot.SchedulerCreateRequest
}

func NewActions() *Actions {
	return &Actions{
		Location:     time.FixedZone("Asia/Shanghai", 8*60*60),
		KV:           map[string]any{},
		Config:       map[string]any{},
		GroupMembers: map[string]rayleabot.ActionResult{},
		GroupErrors:  map[string]error{},
	}
}

func (fake *Actions) TimeLocation() *time.Location { return fake.Location }

func (fake *Actions) ThirdPartyAccountRead(context.Context, rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.Accounts, nil
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
	case "thirdparty.account.validate":
		var request AccountValidationRequest
		if err := decodeInput(input, &request); err != nil {
			return fmt.Errorf("unexpected third-party validation input %T", input)
		}
		fake.AccountValidations = append(fake.AccountValidations, request)
		if result, ok := output.(*rayleabot.ActionResult); ok {
			*result = rayleabot.ActionResult{"accepted": true, "reason": "queued"}
		}
		return nil
	case "thirdparty.resolve":
		var request ResolveRequest
		if err := decodeInput(input, &request); err != nil {
			return fmt.Errorf("unexpected third-party resolve input %T", input)
		}
		fake.ResolveRequests = append(fake.ResolveRequests, request)
		if result, ok := output.(*rayleabot.ActionResult); ok {
			if len(fake.ResolveResults) > 0 {
				*result = fake.ResolveResults[0]
				fake.ResolveResults = fake.ResolveResults[1:]
			} else {
				*result = rayleabot.ActionResult{}
			}
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
