package plugin

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	weiboMediaTimeoutSeconds = 5
	weiboMediaTotalTimeout   = 8 * time.Second
	maxWeiboMediaItemBytes   = 160 << 10
	maxWeiboMediaBatchBytes  = 320 << 10
)

type weiboMediaCacheEntry struct {
	ready chan struct{}
	data  string
	size  int
}

type weiboMediaCache struct {
	entries sync.Map
}

type weiboMediaResolution struct {
	data string
	size int
}

func inlineWeiboUpdateMedia(ctx context.Context, actions pluginActions, data map[string]any, cache *weiboMediaCache) (int, int) {
	mediaCtx, cancel := context.WithTimeout(ctx, weiboMediaTotalTimeout)
	defer cancel()
	if cache == nil {
		cache = &weiboMediaCache{}
	}

	type mediaSection struct {
		owner map[string]any
		items []map[string]any
	}
	sections := []mediaSection{{owner: data, items: mapSliceValue(data["media_items"])}}
	if original := mapValue(data["original"]); original != nil {
		sections = append(sections, mediaSection{owner: original, items: mapSliceValue(original["media_items"])})
	}
	remoteItemCount := 0
	for _, section := range sections {
		for _, item := range section.items {
			if strings.HasPrefix(strings.TrimSpace(stringScalar(item["url"])), "https://") {
				remoteItemCount++
			}
		}
	}
	itemByteLimit := maxWeiboMediaItemBytes
	if remoteItemCount > 0 && maxWeiboMediaBatchBytes/remoteItemCount < itemByteLimit {
		itemByteLimit = maxWeiboMediaBatchBytes / remoteItemCount
	}

	resolutions := map[string]weiboMediaResolution{}
	var resolutionMu sync.Mutex
	var wait sync.WaitGroup
	seen := map[string]struct{}{}
	for _, section := range sections {
		count := len(section.items)
		for _, item := range section.items {
			sourceURL := strings.TrimSpace(stringScalar(item["url"]))
			if !strings.HasPrefix(sourceURL, "https://") {
				continue
			}
			key := weiboMediaCacheKey(sourceURL, count, itemByteLimit)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			wait.Add(1)
			go func(key, sourceURL string, count, maxBytes int) {
				defer wait.Done()
				dataURL, size := cache.resolve(mediaCtx, actions, sourceURL, count, maxBytes)
				resolutionMu.Lock()
				resolutions[key] = weiboMediaResolution{data: dataURL, size: size}
				resolutionMu.Unlock()
			}(key, sourceURL, count, itemByteLimit)
		}
	}
	wait.Wait()

	usedBytes := 0
	resolvedCount := 0
	failedCount := 0
	for _, section := range sections {
		kept := make([]map[string]any, 0, len(section.items))
		for _, item := range section.items {
			sourceURL := strings.TrimSpace(stringScalar(item["url"]))
			if sourceURL == "" {
				item["url"] = firstText(item["fallback"], "assets/grid.svg")
				kept = append(kept, item)
				continue
			}
			if strings.HasPrefix(sourceURL, "data:") || !strings.HasPrefix(sourceURL, "https://") {
				kept = append(kept, item)
				continue
			}
			resolved := resolutions[weiboMediaCacheKey(sourceURL, len(section.items), itemByteLimit)]
			if resolved.data == "" || resolved.size <= 0 || usedBytes+resolved.size > maxWeiboMediaBatchBytes {
				failedCount++
				item["url"] = firstText(item["fallback"], "assets/grid.svg")
				kept = append(kept, item)
				continue
			}
			item["url"] = resolved.data
			usedBytes += resolved.size
			resolvedCount++
			kept = append(kept, item)
		}
		section.owner["media_items"] = kept
		section.owner["image_count"] = len(kept)
		section.owner["media_grid_class"] = mediaGridClass(len(kept))
	}
	return resolvedCount, failedCount
}

func (cache *weiboMediaCache) resolve(ctx context.Context, actions pluginActions, sourceURL string, count, maxBytes int) (string, int) {
	if ctx.Err() != nil {
		return "", 0
	}
	key := weiboMediaCacheKey(sourceURL, count, maxBytes)
	entry := &weiboMediaCacheEntry{ready: make(chan struct{})}
	actual, loaded := cache.entries.LoadOrStore(key, entry)
	if loaded {
		cached := actual.(*weiboMediaCacheEntry)
		select {
		case <-cached.ready:
			return cached.data, cached.size
		case <-ctx.Done():
			return "", 0
		}
	}
	defer close(entry.ready)
	for _, candidate := range weiboMediaCandidateURLs(sourceURL, count) {
		dataURL, size, err := resolveAvatarDataURLLimited(ctx, actions, candidate, weiboMediaTimeoutSeconds, maxBytes)
		if err == nil {
			entry.data, entry.size = dataURL, size
			break
		}
	}
	if entry.data == "" {
		cache.entries.Delete(key)
	}
	return entry.data, entry.size
}

func weiboMediaCacheKey(sourceURL string, count, maxBytes int) string {
	return strings.TrimSpace(sourceURL) + "|" + strconv.Itoa(count) + "|" + strconv.Itoa(maxBytes)
}

func weiboMediaCandidateURLs(sourceURL string, count int) []string {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".sinaimg.cn") {
		return nil
	}
	sizes := []string{"orj360", "thumbnail"}
	if count == 1 {
		sizes = []string{"mw690", "orj360", "thumbnail"}
	}
	result := make([]string, 0, len(sizes)+1)
	seen := map[string]struct{}{}
	for _, size := range sizes {
		candidate := parsed.String()
		if weiboOrjSizePattern.MatchString(parsed.Path) {
			clone := *parsed
			clone.Path = weiboOrjSizePattern.ReplaceAllString(parsed.Path, "/"+size+"/")
			clone.RawPath = ""
			candidate = clone.String()
		}
		if _, exists := seen[candidate]; !exists {
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	if _, exists := seen[parsed.String()]; !exists {
		result = append(result, parsed.String())
	}
	return result
}
