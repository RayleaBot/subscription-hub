package testkit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type HTTPRoute struct {
	Path          string
	QueryContains string
	Result        rayleabot.ActionResult
	Err           error
}

func (fake *Actions) HTTPRequest(_ context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.HTTPRequests = append(fake.HTTPRequests, request)
	if fake.HTTPRoutes != nil {
		if parsed, parseErr := url.Parse(request.URL); parseErr == nil {
			for _, route := range fake.HTTPRoutes {
				if parsed.Path == route.Path && (route.QueryContains == "" || strings.Contains(request.URL, route.QueryContains)) {
					return route.Result, route.Err
				}
			}
		}
	}
	if fake.HTTPFallback != nil {
		if result, err, handled := fake.HTTPFallback(request); handled {
			return result, err
		}
	}
	if len(fake.HTTPErrors) > 0 {
		err := fake.HTTPErrors[0]
		fake.HTTPErrors = fake.HTTPErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(fake.HTTPResponses) == 0 {
		if fake.HTTPDefault != nil {
			return fake.HTTPDefault, nil
		}
		return HTTPJSON(200, map[string]any{}), nil
	}
	result := fake.HTTPResponses[0]
	fake.HTTPResponses = fake.HTTPResponses[1:]
	return result, nil
}

func HTTPJSON(status int, document map[string]any) rayleabot.ActionResult {
	raw, _ := json.Marshal(document)
	return rayleabot.ActionResult{"status_code": status, "body_text": string(raw), "headers": map[string]any{}}
}

type FailedAvatarActions struct {
	*Actions
	Requests []rayleabot.HTTPRequest
}

func (actions *FailedAvatarActions) HTTPRequest(_ context.Context, request rayleabot.HTTPRequest) (rayleabot.ActionResult, error) {
	actions.mu.Lock()
	defer actions.mu.Unlock()
	actions.Requests = append(actions.Requests, request)
	return nil, context.DeadlineExceeded
}

func AvatarHTTPResult() rayleabot.ActionResult {
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	return rayleabot.ActionResult{
		"status_code": 200,
		"headers":     map[string]any{"Content-Type": "image/png"},
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}
}

func RequestURLs(fake *Actions) []string {
	urls := make([]string, 0, len(fake.HTTPRequests))
	for _, request := range fake.HTTPRequests {
		urls = append(urls, request.Method+" "+request.URL)
	}
	return urls
}
