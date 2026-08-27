package testkit

import (
	"context"
	"sort"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

func (fake *Actions) KVGet(_ context.Context, key string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	value, exists := fake.KV[key]
	if !exists {
		return rayleabot.ActionResult{"exists": false}, nil
	}
	return rayleabot.ActionResult{"exists": true, "value": value}, nil
}

func (fake *Actions) KVSet(_ context.Context, key string, value any) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.KV[key] = value
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *Actions) KVDelete(_ context.Context, key string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	delete(fake.KV, key)
	return rayleabot.ActionResult{"ok": true}, nil
}

func (fake *Actions) KVList(_ context.Context, prefix string) (rayleabot.ActionResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	matched := make([]string, 0)
	for key := range fake.KV {
		if strings.HasPrefix(key, prefix) {
			matched = append(matched, key)
		}
	}
	sort.Strings(matched)
	keys := make([]any, 0, len(matched))
	for _, key := range matched {
		keys = append(keys, key)
	}
	return rayleabot.ActionResult{"prefix": prefix, "keys": keys}, nil
}
