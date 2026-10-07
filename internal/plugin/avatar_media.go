package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/httpaction"
)

const maxAvatarResolveItems = 4

const maxAvatarBytes = 512 << 10

const MaxUpdateCardAvatarBytes = 48 << 10

const maxAvatarBatchBytes = 2 << 20

const managementAvatarTimeoutSeconds = 8

const managementAvatarTotalTimeout = 10 * time.Second

var errUnsupportedAvatarURL = errors.New("unsupported avatar url")

type resolvedAvatar struct {
	SourceURL string `json:"source_url"`
	DataURL   string `json:"data_url"`
}

func (handler *Handler) ResolveAvatarDataURLs(ctx context.Context, actions SourceActions, payload map[string]any) map[string]any {
	urls := avatarURLsFromPayload(payload)
	type avatarResult struct {
		item  *resolvedAvatar
		size  int
		issue map[string]any
	}
	results := make([]avatarResult, len(urls))
	resolveCtx, cancel := context.WithTimeout(ctx, managementAvatarTotalTimeout)
	defer cancel()
	var wait sync.WaitGroup
	for index, sourceURL := range urls {
		wait.Add(1)
		go func(index int, sourceURL string) {
			defer wait.Done()
			dataURL, size, err := resolveAvatarDataURL(resolveCtx, actions, sourceURL, handler.avatarPolicies()...)
			if err != nil {
				results[index].issue = map[string]any{"source_url": sourceURL, "message": friendlyAvatarError(err)}
				return
			}
			results[index].item = &resolvedAvatar{SourceURL: sourceURL, DataURL: dataURL}
			results[index].size = size
		}(index, sourceURL)
	}
	wait.Wait()

	items := make([]resolvedAvatar, 0, len(urls))
	issues := make([]map[string]any, 0)
	totalBytes := 0
	for _, result := range results {
		if result.issue != nil {
			issues = append(issues, result.issue)
			continue
		}
		if result.item == nil {
			continue
		}
		if totalBytes+result.size > maxAvatarBatchBytes {
			issues = append(issues, map[string]any{"source_url": result.item.SourceURL, "message": "头像批次超过大小限制"})
			continue
		}
		totalBytes += result.size
		items = append(items, *result.item)
	}
	return map[string]any{"items": items, "issues": issues}
}

func avatarURLsFromPayload(payload map[string]any) []string {
	values := SliceValue(payload["urls"])
	result := make([]string, 0, min(len(values), maxAvatarResolveItems))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sourceURL := strings.TrimSpace(StringScalar(value))
		if sourceURL == "" {
			continue
		}
		if _, exists := seen[sourceURL]; exists {
			continue
		}
		seen[sourceURL] = struct{}{}
		result = append(result, sourceURL)
		if len(result) == maxAvatarResolveItems {
			break
		}
	}
	return result
}

func resolveAvatarDataURL(ctx context.Context, actions SourceActions, sourceURL string, policies ...AvatarPolicy) (string, int, error) {
	return resolveAvatarDataURLWithCandidates(ctx, actions, sourceURL, managementAvatarTimeoutSeconds, maxAvatarBytes, policies...)
}

// resolveAvatarDataURLWithCandidates 先按策略校验，再依次尝试策略提供的镜像候选
// （如 douyin 的区域 CDN 主机），任一成功即返回，避免单区域偶发失败导致头像缺失。
func resolveAvatarDataURLWithCandidates(ctx context.Context, actions SourceActions, sourceURL string, timeoutSeconds, maxBytes int, policies ...AvatarPolicy) (string, int, error) {
	parsed, _, err := ValidateAvatarSourceURL(sourceURL, policies...)
	if err != nil {
		return "", 0, err
	}
	candidates := []string{parsed.String()}
	for _, policy := range policies {
		if policy.Validate == nil || policy.Candidates == nil {
			continue
		}
		if _, ok := policy.Validate(parsed); ok {
			if policyCandidates := policy.Candidates(parsed); len(policyCandidates) > 0 {
				candidates = policyCandidates
			}
			break
		}
	}
	var lastError error
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] || ctx.Err() != nil {
			continue
		}
		seen[candidate] = true
		dataURL, size, err := ResolveAvatarDataURLLimited(ctx, actions, candidate, timeoutSeconds, maxBytes, policies...)
		if err == nil {
			return dataURL, size, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = context.Cause(ctx)
	}
	if lastError == nil {
		lastError = errUnsupportedAvatarURL
	}
	return "", 0, lastError
}

func ResolveAvatarDataURLWithTimeout(ctx context.Context, actions SourceActions, sourceURL string, timeoutSeconds int, policies ...AvatarPolicy) (string, int, error) {
	return ResolveAvatarDataURLLimited(ctx, actions, sourceURL, timeoutSeconds, maxAvatarBytes, policies...)
}

func ResolveAvatarDataURLLimited(ctx context.Context, actions SourceActions, sourceURL string, timeoutSeconds, maxBytes int, policies ...AvatarPolicy) (string, int, error) {
	parsed, referer, err := ValidateAvatarSourceURL(sourceURL, policies...)
	if err != nil {
		return "", 0, err
	}
	if maxBytes <= 0 {
		maxBytes = maxAvatarBytes
	}
	result, err := actions.HTTPRequest(ctx, httpaction.Request{
		Method: "GET",
		URL:    parsed.String(),
		Headers: map[string]string{
			"Accept":        "image/avif,image/webp,image/apng,image/png,image/jpeg,image/gif,*/*;q=0.1",
			"Cache-Control": "no-cache",
			"Pragma":        "no-cache",
			"Referer":       referer,
			"User-Agent":    BrowserUserAgent,
		},
		TimeoutSeconds: timeoutSeconds,
	})
	if err != nil {
		return "", 0, fmt.Errorf("fetch avatar: %w", err)
	}
	if IntScalar(result["status_code"]) != http.StatusOK {
		return "", 0, fmt.Errorf("fetch avatar: upstream status %d", IntScalar(result["status_code"]))
	}
	body, err := avatarResponseBody(result)
	if err != nil {
		return "", 0, err
	}
	if len(body) == 0 || len(body) > maxBytes {
		return "", 0, fmt.Errorf("fetch avatar: invalid body size")
	}
	mimeType := avatarContentType(result, body)
	if !supportedAvatarContentType(mimeType) {
		return "", 0, fmt.Errorf("fetch avatar: unsupported content type")
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(body), len(body), nil
}

func ValidateAvatarSourceURL(sourceURL string, policies ...AvatarPolicy) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host == "" || parsed.Port() != "" {
		return nil, "", errUnsupportedAvatarURL
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "q1.qlogo.cn":
		query := parsed.Query()
		if parsed.EscapedPath() != "/g" || query.Get("b") != "qq" || Digits(query.Get("nk")) == "" || !avatarSizeAllowed(query.Get("s")) {
			return nil, "", errUnsupportedAvatarURL
		}
		for key := range query {
			if key != "b" && key != "nk" && key != "s" {
				return nil, "", errUnsupportedAvatarURL
			}
		}
		return parsed, "https://qzone.qq.com/", nil
	case "p.qlogo.cn":
		parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
		if parsed.RawQuery != "" || len(parts) != 4 || parts[0] != "gh" || Digits(parts[1]) == "" || parts[1] != parts[2] || !avatarSizeAllowed(parts[3]) {
			return nil, "", errUnsupportedAvatarURL
		}
		return parsed, "https://qun.qq.com/", nil
	}
	for _, policy := range policies {
		if policy.Validate != nil {
			if referer, allowed := policy.Validate(parsed); allowed {
				return parsed, referer, nil
			}
		}
	}
	return nil, "", errUnsupportedAvatarURL
}

func avatarSizeAllowed(value string) bool {
	switch strings.TrimSpace(value) {
	case "40", "100", "140", "640":
		return true
	default:
		return false
	}
}

func avatarResponseBody(result rayleabot.ActionResult) ([]byte, error) {
	if encoded := StringScalar(result["body_base64"]); encoded != "" {
		body, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("fetch avatar: invalid base64 body")
		}
		return body, nil
	}
	if text, exists := result["body_text"].(string); exists && text != "" {
		return []byte(text), nil
	}
	return nil, fmt.Errorf("fetch avatar: empty body")
}

func avatarContentType(result rayleabot.ActionResult, body []byte) string {
	headers := MapValue(result["headers"])
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			return strings.ToLower(strings.TrimSpace(strings.Split(StringScalar(value), ";")[0]))
		}
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(body), ";")[0]))
}

func supportedAvatarContentType(value string) bool {
	switch value {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/avif":
		return true
	default:
		return false
	}
}

func friendlyAvatarError(err error) string {
	if errors.Is(err, errUnsupportedAvatarURL) {
		return "头像地址不受支持"
	}
	return "头像读取失败"
}

func (handler *Handler) avatarPolicies() []AvatarPolicy {
	policies := make([]AvatarPolicy, 0, len(handler.platforms))
	for _, platform := range handler.platforms {
		policies = append(policies, platform.Avatar)
	}
	return policies
}

func InlineAvatar(ctx context.Context, actions SourceActions, sourceURL string, timeoutSeconds, maxBytes int, policies ...AvatarPolicy) string {
	parsed, _, err := ValidateAvatarSourceURL(sourceURL, policies...)
	if err != nil || ctx.Err() != nil {
		return ""
	}
	candidates := []string{parsed.String()}
	for _, policy := range policies {
		if policy.Validate == nil || policy.Candidates == nil {
			continue
		}
		if _, ok := policy.Validate(parsed); ok {
			candidates = policy.Candidates(parsed)
			break
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] || ctx.Err() != nil {
			continue
		}
		seen[candidate] = true
		if value, _, err := ResolveAvatarDataURLLimited(ctx, actions, candidate, timeoutSeconds, maxBytes, policies...); err == nil {
			return value
		}
	}
	return ""
}
