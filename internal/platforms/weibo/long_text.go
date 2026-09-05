package weibo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
)

const weiboLongTextTotalTimeout = 6 * time.Second

const weiboLongTextFailureText = "部分微博正文获取不完整，卡片已保留原微博链接。"

type weiboLongTextCacheEntry struct {
	text string
	err  error
}

type weiboLongTextResolver struct {
	actions  plugin.SourceActions
	accounts []weiboAccount
	cache    map[string]weiboLongTextCacheEntry
}

func newWeiboLongTextResolver(actions plugin.SourceActions, accounts []weiboAccount) *weiboLongTextResolver {
	return &weiboLongTextResolver{
		actions:  actions,
		accounts: append([]weiboAccount(nil), accounts...),
		cache:    map[string]weiboLongTextCacheEntry{},
	}
}

func (resolver *weiboLongTextResolver) Prepare(ctx context.Context, update map[string]any) (map[string]any, bool) {
	prepared := plugin.CloneJSONMap(update)
	if prepared == nil {
		return update, false
	}
	degraded := resolver.expand(ctx, prepared)
	if original := plugin.MapValue(prepared["original"]); original != nil {
		degraded = resolver.expand(ctx, original) || degraded
	}
	return prepared, degraded
}

func (resolver *weiboLongTextResolver) expand(ctx context.Context, update map[string]any) bool {
	if resolver == nil || update == nil || !plugin.BoolScalar(update["needs_long_text"]) {
		return false
	}
	text, err := resolver.resolve(ctx, plugin.StringScalar(update["id"]))
	if err == nil && text != "" {
		update["summary"] = text
		update["needs_long_text"] = false
		return false
	}
	update["summary"] = weiboIncompleteSummary(plugin.StringScalar(update["summary"]))
	return true
}

func (resolver *weiboLongTextResolver) resolve(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if cached, exists := resolver.cache[id]; exists {
		return cached.text, cached.err
	}
	if id == "" {
		err := errors.New("微博正文缺少内容标识")
		resolver.cache[id] = weiboLongTextCacheEntry{err: err}
		resolver.logFailure(ctx, id, err)
		return "", err
	}

	requestCtx, cancel := context.WithTimeout(ctx, weiboLongTextTotalTimeout)
	defer cancel()
	document, err := requestWeiboAcrossAccounts(
		requestCtx,
		resolver.actions,
		resolver.accounts,
		weiboLongTextEndpoint(id),
		"https://m.weibo.cn/status/"+id,
	)
	text := ""
	if err == nil {
		text = weiboPlainText(plugin.FirstText(
			plugin.NestedValue(document, "data", "longTextContent"),
			plugin.NestedValue(document, "data", "long_text_content"),
			document["longTextContent"],
			document["long_text_content"],
		))
		if text == "" {
			err = errors.New("微博正文响应缺少正文")
		}
	}
	resolver.cache[id] = weiboLongTextCacheEntry{text: text, err: err}
	if err != nil {
		resolver.logFailure(ctx, id, err)
	}
	return text, err
}

func (resolver *weiboLongTextResolver) logFailure(ctx context.Context, id string, err error) {
	if resolver == nil || resolver.actions == nil {
		return
	}
	fields := map[string]any{"update_id": strings.TrimSpace(id)}
	for key, value := range weiboErrorLogFields(err) {
		fields[key] = value
	}
	reason := strings.TrimSpace(friendlyWeiboSourceError("微博正文获取失败", err))
	message := fmt.Sprintf("微博 %s 的完整正文获取失败，改用摘要：%s", strings.TrimSpace(id), reason)
	_, _ = resolver.actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "warn", Message: message, Fields: fields,
	})
}
