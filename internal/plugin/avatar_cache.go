package plugin

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

const UpdateAvatarTimeoutSeconds = 3

const SlowAvatarTimeoutSeconds = 8

const UpdateAvatarTotalTimeout = 10 * time.Second

type avatarCacheEntry struct {
	ready chan struct{}
	value string
}

// AvatarCache coalesces concurrent loads within one operation and retries failures.
type AvatarCache struct {
	entries sync.Map
}

func (cache *AvatarCache) resolve(ctx context.Context, actions SourceActions, sourceURL string, policies ...AvatarPolicy) string {
	if ctx.Err() != nil {
		return ""
	}
	if cache == nil {
		return InlineAvatar(ctx, actions, sourceURL, updateAvatarTimeoutSeconds(sourceURL), MaxUpdateCardAvatarBytes, policies...)
	}
	entry := &avatarCacheEntry{ready: make(chan struct{})}
	actual, loaded := cache.entries.LoadOrStore(strings.TrimSpace(sourceURL), entry)
	if loaded {
		cached := actual.(*avatarCacheEntry)
		select {
		case <-cached.ready:
			return cached.value
		case <-ctx.Done():
			return ""
		}
	}
	entry.value = InlineAvatar(ctx, actions, sourceURL, updateAvatarTimeoutSeconds(sourceURL), MaxUpdateCardAvatarBytes, policies...)
	if entry.value == "" {
		cache.entries.Delete(strings.TrimSpace(sourceURL))
	}
	close(entry.ready)
	return entry.value
}

func updateAvatarTimeoutSeconds(sourceURL string) int {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil {
		return UpdateAvatarTimeoutSeconds
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "douyinpic.com" || strings.HasSuffix(host, ".douyinpic.com") {
		return SlowAvatarTimeoutSeconds
	}
	return UpdateAvatarTimeoutSeconds
}

// InlineUpdateAvatars 并发内联卡片中的作者、原作者与订阅人头像。
// 内联失败时对应字段置空，模板回退到内置默认头像，避免远程图片与截图竞态。
func (handler *Handler) InlineUpdateAvatars(ctx context.Context, actions SourceActions, data map[string]any, shared ...*AvatarCache) {
	cache := &AvatarCache{}
	if len(shared) > 0 && shared[0] != nil {
		cache = shared[0]
	}
	handler.inlineUpdateAvatarsWithSharedCache(ctx, actions, data, cache)
}

func (handler *Handler) inlineUpdateAvatarsWithSharedCache(ctx context.Context, actions SourceActions, data map[string]any, cache *AvatarCache) {
	avatarCtx, cancel := context.WithTimeout(ctx, UpdateAvatarTotalTimeout)
	defer cancel()
	handler.InlineUpdateAvatarsWithCache(avatarCtx, actions, data, cache)
}

func (handler *Handler) InlineUpdateAvatarsWithCache(ctx context.Context, actions SourceActions, data map[string]any, cache *AvatarCache) {
	type avatarField struct {
		object map[string]any
		key    string
	}
	fields := make([]avatarField, 0, 4)
	if author := MapValue(data["author"]); author != nil {
		fields = append(fields, avatarField{author, "avatar"})
	}
	if original := MapValue(data["original"]); original != nil {
		if author := MapValue(original["author"]); author != nil {
			fields = append(fields, avatarField{author, "avatar"})
		}
	}
	for _, cardMap := range MapSliceValue(data["subscriber_cards"]) {
		fields = append(fields, avatarField{cardMap, "avatar_url"})
	}
	var wait sync.WaitGroup
	for _, field := range fields {
		sourceURL := StringScalar(field.object[field.key])
		if !strings.HasPrefix(sourceURL, "https://") && !strings.HasPrefix(sourceURL, "http://") {
			// 本地资源（如预览示例的 assets 路径）保持原样。
			continue
		}
		wait.Add(1)
		go func(field avatarField, sourceURL string) {
			defer wait.Done()
			field.object[field.key] = cache.resolve(ctx, actions, sourceURL, handler.avatarPolicies()...)
		}(field, sourceURL)
	}
	wait.Wait()
}
