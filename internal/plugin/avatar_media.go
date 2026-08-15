package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	maxAvatarResolveItems       = 4
	maxAvatarBytes              = 512 << 10
	maxUpdateCardAvatarBytes    = 48 << 10
	maxAvatarBatchBytes         = 2 << 20
	defaultAvatarTimeoutSeconds = 3
)

var errUnsupportedAvatarURL = errors.New("unsupported avatar url")

type resolvedAvatar struct {
	SourceURL string `json:"source_url"`
	DataURL   string `json:"data_url"`
}

func resolveAvatarDataURLs(ctx context.Context, actions pluginActions, payload map[string]any) map[string]any {
	urls := avatarURLsFromPayload(payload)
	items := make([]resolvedAvatar, 0, len(urls))
	issues := make([]map[string]any, 0)
	totalBytes := 0
	for _, sourceURL := range urls {
		dataURL, size, err := resolveAvatarDataURL(ctx, actions, sourceURL)
		if err != nil {
			issues = append(issues, map[string]any{"source_url": sourceURL, "message": friendlyAvatarError(err)})
			continue
		}
		if totalBytes+size > maxAvatarBatchBytes {
			issues = append(issues, map[string]any{"source_url": sourceURL, "message": "头像批次超过大小限制"})
			continue
		}
		totalBytes += size
		items = append(items, resolvedAvatar{SourceURL: sourceURL, DataURL: dataURL})
	}
	return map[string]any{"items": items, "issues": issues}
}

func avatarURLsFromPayload(payload map[string]any) []string {
	values := sliceValue(payload["urls"])
	result := make([]string, 0, min(len(values), maxAvatarResolveItems))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sourceURL := strings.TrimSpace(stringScalar(value))
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

func resolveAvatarDataURL(ctx context.Context, actions pluginActions, sourceURL string) (string, int, error) {
	return resolveAvatarDataURLWithTimeout(ctx, actions, sourceURL, defaultAvatarTimeoutSeconds)
}

func resolveAvatarDataURLWithTimeout(ctx context.Context, actions pluginActions, sourceURL string, timeoutSeconds int) (string, int, error) {
	return resolveAvatarDataURLLimited(ctx, actions, sourceURL, timeoutSeconds, maxAvatarBytes)
}

func resolveAvatarDataURLLimited(ctx context.Context, actions pluginActions, sourceURL string, timeoutSeconds, maxBytes int) (string, int, error) {
	parsed, referer, err := validateAvatarSourceURL(sourceURL)
	if err != nil {
		return "", 0, err
	}
	if maxBytes <= 0 {
		maxBytes = maxAvatarBytes
	}
	result, err := actions.HTTPRequest(ctx, rayleabot.HTTPRequest{
		Method: "GET",
		URL:    parsed.String(),
		Headers: map[string]string{
			"Accept":        "image/avif,image/webp,image/apng,image/png,image/jpeg,image/gif,*/*;q=0.1",
			"Cache-Control": "no-cache",
			"Pragma":        "no-cache",
			"Referer":       referer,
			"User-Agent":    bilibiliUserAgent,
		},
		TimeoutSeconds: timeoutSeconds,
	})
	if err != nil {
		return "", 0, fmt.Errorf("fetch avatar: %w", err)
	}
	if intScalar(result["status_code"]) != http.StatusOK {
		return "", 0, fmt.Errorf("fetch avatar: upstream status %d", intScalar(result["status_code"]))
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

func validateAvatarSourceURL(sourceURL string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host == "" || parsed.Port() != "" {
		return nil, "", errUnsupportedAvatarURL
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "i0.hdslb.com", "i1.hdslb.com", "i2.hdslb.com":
		path := parsed.EscapedPath()
		if parsed.RawQuery != "" || (!strings.HasPrefix(path, "/bfs/face/") && !strings.HasPrefix(path, "/bfs/garb/")) {
			return nil, "", errUnsupportedAvatarURL
		}
		return parsed, "https://www.bilibili.com/", nil
	case "q1.qlogo.cn":
		query := parsed.Query()
		if parsed.EscapedPath() != "/g" || query.Get("b") != "qq" || digits(query.Get("nk")) == "" || !avatarSizeAllowed(query.Get("s")) {
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
		if parsed.RawQuery != "" || len(parts) != 4 || parts[0] != "gh" || digits(parts[1]) == "" || parts[1] != parts[2] || !avatarSizeAllowed(parts[3]) {
			return nil, "", errUnsupportedAvatarURL
		}
		return parsed, "https://qun.qq.com/", nil
	case "tva1.sinaimg.cn", "tva2.sinaimg.cn", "tva3.sinaimg.cn", "tva4.sinaimg.cn",
		"tvax1.sinaimg.cn", "tvax2.sinaimg.cn", "tvax3.sinaimg.cn", "tvax4.sinaimg.cn",
		"wx1.sinaimg.cn", "wx2.sinaimg.cn", "wx3.sinaimg.cn", "wx4.sinaimg.cn":
		return parsed, "https://weibo.com/", nil
	default:
		return nil, "", errUnsupportedAvatarURL
	}
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
	if encoded := stringScalar(result["body_base64"]); encoded != "" {
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
	headers := mapValue(result["headers"])
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			return strings.ToLower(strings.TrimSpace(strings.Split(stringScalar(value), ";")[0]))
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
