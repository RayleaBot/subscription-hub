package plugin

import (
	"context"
	"errors"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	weiboLongTextTotalTimeout = 6 * time.Second
	weiboLongTextFailureText  = "部分微博正文获取不完整，卡片已保留原微博链接。"
)

type weiboLongTextCacheEntry struct {
	text string
	err  error
}

type weiboLongTextResolver struct {
	actions  pluginActions
	accounts []weiboAccount
	cache    map[string]weiboLongTextCacheEntry
}

func newWeiboLongTextResolver(actions pluginActions, accounts []weiboAccount) *weiboLongTextResolver {
	return &weiboLongTextResolver{
		actions:  actions,
		accounts: append([]weiboAccount(nil), accounts...),
		cache:    map[string]weiboLongTextCacheEntry{},
	}
}

func (resolver *weiboLongTextResolver) prepare(ctx context.Context, update map[string]any) (map[string]any, bool) {
	prepared := cloneJSONMap(update)
	if prepared == nil {
		return update, false
	}
	degraded := resolver.expand(ctx, prepared)
	if original := mapValue(prepared["original"]); original != nil {
		degraded = resolver.expand(ctx, original) || degraded
	}
	return prepared, degraded
}

func (resolver *weiboLongTextResolver) expand(ctx context.Context, update map[string]any) bool {
	if resolver == nil || update == nil || !boolScalar(update["needs_long_text"]) {
		return false
	}
	text, err := resolver.resolve(ctx, stringScalar(update["id"]))
	if err == nil && text != "" {
		update["summary"] = text
		update["needs_long_text"] = false
		return false
	}
	update["summary"] = weiboIncompleteSummary(stringScalar(update["summary"]))
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
		text = weiboPlainText(firstText(
			nestedValue(document, "data", "longTextContent"),
			nestedValue(document, "data", "long_text_content"),
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
	for key, value := range actionErrorLogFields(err) {
		fields[key] = value
	}
	var sourceErr *weiboSourceError
	if errors.As(err, &sourceErr) {
		if sourceErr.Kind != "" {
			fields["error_kind"] = sourceErr.Kind
		}
		if sourceErr.HTTPStatus != 0 {
			fields["http_status"] = sourceErr.HTTPStatus
		}
		if sourceErr.Message != "" {
			fields["diagnostic"] = weiboDiagnosticExcerpt(sourceErr.Message, 240)
		}
	}
	_, _ = resolver.actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level: "warn", Message: "微博正文获取不完整", Fields: fields,
	})
}
