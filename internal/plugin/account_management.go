package plugin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	accountErrorInvalid        = "account.invalid_request"
	accountErrorNotFound       = "account.not_found"
	accountErrorExists         = "account.already_exists"
	accountErrorUnsupported    = "account.unsupported"
	accountErrorBrowserBusy    = "account.browser_busy"
	accountErrorBrowserMissing = "account.browser_unavailable"
	accountErrorSessionMiss    = "account.session_not_found"
	accountErrorFailed         = "account.failed"
)

func (handler *Handler) initAccountQRManager() {
	providers := map[string]QRLoginProviderFactory{}
	for _, platform := range handler.platforms {
		if platform.Account != nil && platform.Account.NewQRProvider != nil {
			providers[platform.ID] = platform.Account.NewQRProvider
		}
	}
	handler.accountQR = NewQRLoginManager(providers, handler.now, handler.persistQRLoginAccount)
}

func (handler *Handler) persistQRLoginAccount(ctx context.Context, actions RuntimeActions, platform, cookie string, profile AccountProfile, now time.Time) (Account, error) {
	accountID := QRLoginAccountID(profile)
	account := Account{
		Platform:            platform,
		AccountID:           accountID,
		Label:               profile.Nickname,
		Enabled:             true,
		CredentialState:     CredentialValid,
		CredentialCheckedAt: now.UTC().Format(time.RFC3339),
	}
	account = ApplyProfile(account, profile)
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	if existing, exists, err := getAccount(ctx, actions, platform, accountID); err != nil {
		return Account{}, err
	} else if exists {
		account.Label, account.Enabled = existing.Label, existing.Enabled
	}
	return saveAccount(ctx, actions, account, cookie, now)
}

func (handler *Handler) accountPlatform(id string) (Platform, bool) {
	platform, ok := handler.byID[strings.ToLower(strings.TrimSpace(id))]
	return platform, ok
}

func (handler *Handler) accountList(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	if platform != "" {
		if definition, ok := handler.accountPlatform(platform); !ok || definition.Account == nil {
			return accountErrorResult(accountErrorUnsupported, "不支持的平台。"), nil
		}
	}
	accounts, err := ListAccounts(ctx, actions, platform)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(strings.TrimSpace(stringValue(payload, "query", "")))
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		definition, ok := handler.accountPlatform(account.Platform)
		if !ok || definition.Account == nil {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(strings.Join([]string{account.Platform, definition.Name, account.AccountID, account.Label, account.Nickname, account.UID}, " ")), query) {
			continue
		}
		filtered = append(filtered, account)
	}
	slices.SortFunc(filtered, func(a, b Account) int { return strings.Compare(a.Key(), b.Key()) })
	total := len(filtered)
	offset := max(0, int(IntScalar(payload["offset"])))
	limit := int(IntScalar(payload["limit"]))
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	offset = min(offset, total)
	return map[string]any{"accounts": filtered[offset:min(offset+limit, total)], "platforms": handler.accountPlatformsView(), "total": total, "offset": offset, "limit": limit}, nil
}

type accountPlatformView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (handler *Handler) accountPlatformsView() []accountPlatformView {
	result := make([]accountPlatformView, 0, len(handler.platforms))
	for _, platform := range handler.platforms {
		if platform.Account == nil {
			continue
		}
		result = append(result, accountPlatformView{ID: platform.ID, Name: platform.Name})
	}
	return result
}

func (handler *Handler) accountUpsert(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	accountID := NormalizeAccountID(stringValue(payload, "account_id", ""))
	definition, ok := handler.accountPlatform(platform)
	if !ok || definition.Account == nil {
		return accountErrorResult(accountErrorUnsupported, "不支持的平台。"), nil
	}
	if !ValidAccountID(accountID) {
		return accountErrorResult(accountErrorInvalid, "账号 ID 只允许小写字母、数字、下划线、点或中划线。"), nil
	}
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	existing, exists, err := getAccount(ctx, actions, platform, accountID)
	if err != nil {
		return nil, err
	}
	if BoolScalar(payload["create_only"]) && exists {
		return accountErrorResult(accountErrorExists, "账号 ID 已存在。"), nil
	}
	account := existing
	account.Platform = platform
	account.AccountID = accountID
	if !exists {
		account.Enabled = true
		account.CredentialState = CredentialUnknown
	}
	if raw, present := payload["label"]; present {
		account.Label = StringScalar(raw)
	}
	if raw, present := payload["enabled"]; present {
		account.Enabled = BoolScalar(raw)
	}
	if strings.TrimSpace(account.Label) == "" {
		account.Label = accountID
	}
	if raw, present := payload["cookie"]; present {
		cookie := strings.TrimSpace(StringScalar(raw))
		if cookie == "" && !exists {
			return accountErrorResult(accountErrorInvalid, "新增账号需要填写 CK。"), nil
		}
	}
	cookie := strings.TrimSpace(StringScalar(payload["cookie"]))
	if cookie != "" {
		account.CredentialState, account.CredentialCheckedAt, account.CredentialLastError = CredentialUnknown, "", ""
	}
	if !exists && cookie == "" {
		return accountErrorResult(accountErrorInvalid, "新增账号需要填写 CK。"), nil
	}
	saved, err := saveAccount(ctx, actions, account, cookie, handler.now())
	if err != nil {
		return nil, err
	}
	return map[string]any{"account": saved}, nil
}

func (handler *Handler) accountDelete(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	accountID := NormalizeAccountID(stringValue(payload, "account_id", ""))
	if !ValidAccountID(accountID) {
		return accountErrorResult(accountErrorInvalid, "账号 ID 不合法。"), nil
	}
	if _, ok := handler.accountPlatform(platform); !ok {
		return accountErrorResult(accountErrorUnsupported, "不支持的平台。"), nil
	}
	if err := DeleteAccount(ctx, actions, platform, accountID); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true}, nil
}

func (handler *Handler) accountValidate(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	accountID := NormalizeAccountID(stringValue(payload, "account_id", ""))
	definition, ok := handler.accountPlatform(platform)
	if !ok || definition.Account == nil || definition.Account.Validate == nil {
		return accountErrorResult(accountErrorUnsupported, "该平台暂不支持 CK 检查。"), nil
	}
	if !ValidAccountID(accountID) {
		return accountErrorResult(accountErrorInvalid, "账号 ID 不合法。"), nil
	}
	account, exists, err := GetAccount(ctx, actions, platform, accountID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return accountErrorResult(accountErrorNotFound, "账号不存在。"), nil
	}
	cookie, err := AccountCookie(ctx, actions, account)
	if err != nil {
		return nil, err
	}
	if cookie == "" {
		return accountErrorResult(accountErrorInvalid, "账号尚未配置 CK。"), nil
	}
	validation := definition.Account.Validate(ctx, actions, cookie)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	checkedAt := handler.now().UTC()
	saved, applied, err := commitAccountValidation(ctx, actions, account, validation, checkedAt.Format(time.RFC3339Nano), checkedAt)
	if errors.Is(err, ErrAccountNotFound) {
		return accountErrorResult(accountErrorNotFound, "账号已删除。"), nil
	}
	if err != nil {
		return nil, err
	}
	if applied {
		handler.logAccountValidation(ctx, actions, saved)
	}
	return map[string]any{"account": saved}, nil
}

func (handler *Handler) logAccountValidation(ctx context.Context, actions SourceActions, account Account) {
	if actions == nil {
		return
	}
	message := "账号 CK 检查完成"
	level := "info"
	switch account.CredentialState {
	case CredentialInvalid:
		level = "warn"
		message = "账号 CK 已失效，请重新扫码"
	case CredentialUnknown:
		level = "warn"
		message = "账号 CK 状态暂时无法确认"
	}
	_, _ = actions.LoggerWrite(ctx, rayleabot.LoggerWriteRequest{
		Level:   level,
		Message: message,
		Fields: map[string]any{
			"platform":   account.Platform,
			"account_id": account.AccountID,
			"state":      account.CredentialState,
			"detail":     account.CredentialLastError,
		},
	})
}

func (handler *Handler) accountQRCreate(ctx context.Context, actions RuntimeActions, payload map[string]any, current Settings) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	definition, ok := handler.accountPlatform(platform)
	if !ok || definition.Account == nil || definition.Account.NewQRProvider == nil {
		return accountErrorResult(accountErrorUnsupported, "该平台暂不支持扫码登录。"), nil
	}
	result, err := handler.accountQR.Create(ctx, actions, platform, QRLoginOptions{
		BrowserMode:      NormalizeAccountBrowserMode(current.AccountBrowserMode),
		BrowserRemoteURL: strings.TrimSpace(current.AccountBrowserRemoteURL),
	})
	if err != nil {
		return accountQRErrorResult(err), nil
	}
	return map[string]any{
		"platform":   result.Platform,
		"login_id":   result.LoginID,
		"qrcode_url": result.QRCodeURL,
		"expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
		"state":      result.State,
	}, nil
}

func (handler *Handler) accountQRPoll(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	loginID := strings.TrimSpace(stringValue(payload, "login_id", ""))
	result, err := handler.accountQR.Poll(ctx, actions, platform, loginID)
	if err != nil {
		return accountQRErrorResult(err), nil
	}
	response := map[string]any{
		"platform":   result.Platform,
		"login_id":   result.LoginID,
		"state":      result.State,
		"expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
		"account":    nil,
	}
	if result.SavedAccount != nil {
		response["account"] = result.SavedAccount
	}
	return response, nil
}

func (handler *Handler) accountQRCancel(ctx context.Context, actions RuntimeActions, payload map[string]any) (map[string]any, error) {
	platform := strings.ToLower(strings.TrimSpace(stringValue(payload, "platform", "")))
	loginID := strings.TrimSpace(stringValue(payload, "login_id", ""))
	if err := handler.accountQR.Cancel(ctx, actions, platform, loginID); err != nil {
		return accountQRErrorResult(err), nil
	}
	return map[string]any{"cancelled": true}, nil
}

func accountErrorResult(code, message string) map[string]any {
	return map[string]any{"error": code, "message": message}
}

func accountQRErrorResult(err error) map[string]any {
	switch {
	case err == nil:
		return map[string]any{}
	case strings.Contains(err.Error(), ErrQRLoginUnsupportedPlatform.Error()):
		return accountErrorResult(accountErrorUnsupported, "该平台暂不支持扫码登录。")
	case strings.Contains(err.Error(), ErrQRLoginSessionNotFound.Error()):
		return accountErrorResult(accountErrorSessionMiss, "扫码会话不存在或已结束。")
	case strings.Contains(err.Error(), ErrQRLoginBrowserBusy.Error()):
		return accountErrorResult(accountErrorBrowserBusy, "登录浏览器正被占用，请稍后重试。")
	case strings.Contains(err.Error(), ErrQRLoginBrowserUnavailable.Error()):
		return accountErrorResult(accountErrorBrowserMissing, "登录浏览器不可用，请检查浏览器配置。")
	case strings.Contains(err.Error(), ErrQRLoginCredentialMissing.Error()):
		return accountErrorResult(accountErrorInvalid, "扫码成功但没有取得 CK。")
	default:
		return accountErrorResult(accountErrorFailed, fmt.Sprintf("扫码登录失败：%s", DiagnosticExcerpt(err.Error(), 200)))
	}
}

// The scheduler resumes with accounts that are still due if a check exhausts
// its event budget. Completed checks do not postpone unchecked accounts.
func (handler *Handler) checkDueAccounts(ctx context.Context, actions RuntimeActions, intervalMinutes int) (int, error) {
	if intervalMinutes <= 0 || !handler.accountCheckMu.TryLock() {
		return 0, nil
	}
	defer handler.accountCheckMu.Unlock()
	checked := 0
	accounts, err := ListAccounts(ctx, actions, "")
	if err != nil {
		return 0, err
	}
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return checked, err
		}
		definition, ok := handler.accountPlatform(account.Platform)
		if !ok || definition.Account == nil || definition.Account.Validate == nil || !account.Enabled {
			continue
		}
		now := handler.now().UTC()
		if at, err := time.Parse(time.RFC3339Nano, account.CredentialCheckedAt); err == nil && now.Sub(at) < time.Duration(intervalMinutes)*time.Minute {
			continue
		}
		cookie, err := AccountCookie(ctx, actions, account)
		if err != nil {
			return checked, err
		}
		if cookie == "" {
			continue
		}
		validation := definition.Account.Validate(ctx, actions, cookie)
		if err := ctx.Err(); err != nil {
			return checked, err
		}
		saved, applied, err := commitAccountValidation(ctx, actions, account, validation, handler.now().UTC().Format(time.RFC3339Nano), handler.now())
		if errors.Is(err, ErrAccountNotFound) {
			continue
		}
		if err != nil {
			return checked, err
		}
		if applied {
			handler.logAccountValidation(ctx, actions, saved)
			checked++
		}
	}
	return checked, nil
}
