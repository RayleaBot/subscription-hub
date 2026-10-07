package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

type stubAccountQRProvider struct{}

func (stubAccountQRProvider) LoginIDPrefix() string { return "qr" }

func (stubAccountQRProvider) Create(context.Context, SourceActions, time.Time) (QRLoginSession, error) {
	return QRLoginSession{
		Token:     "stub-token",
		QRCodeURL: "https://example.test/qr",
		ExpiresAt: time.Now().Add(3 * time.Minute),
		State:     QRLoginStatePendingScan,
	}, nil
}

func (stubAccountQRProvider) Poll(context.Context, SourceActions, QRLoginSession, time.Time) (QRLoginSession, error) {
	return QRLoginSession{
		Token:   "stub-token",
		State:   QRLoginStateSucceeded,
		Cookie:  "SESSDATA=fixture;",
		Account: AccountProfile{UID: "qr-user", Nickname: "扫码账号"},
	}, nil
}

func accountTestPlatform(validate func(string) AccountValidation) Platform {
	platform := workflowPlatform("bilibili", &workflowSession{})
	platform.Account = &AccountAdapter{
		Validate: func(_ context.Context, _ SourceActions, cookie string) AccountValidation {
			return validate(cookie)
		},
		NewQRProvider: func(QRLoginOptions) QRLoginProvider { return stubAccountQRProvider{} },
	}
	return platform
}

func TestAccountManagementLifecycle(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, accountTestPlatform(func(cookie string) AccountValidation {
		if cookie != "SESSDATA=fixture;" {
			return AccountValidation{State: CredentialInvalid, Message: "CK 无效"}
		}
		return AccountValidation{State: CredentialValid, Profile: AccountProfile{UID: "123456", Nickname: "测试账号"}}
	}))

	created, err := handler.accountUpsert(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "primary", "label": "主账号", "enabled": true,
		"cookie": "SESSDATA=fixture;", "create_only": true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, failed := created["error"]; failed {
		t.Fatalf("upsert result = %#v", created)
	}
	if actions.Secrets["account.bilibili.primary.cookie"] != "SESSDATA=fixture;" {
		t.Fatalf("cookie was not stored: %#v", actions.Secrets)
	}

	duplicate, err := handler.accountUpsert(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "primary", "label": "重复", "enabled": true,
		"cookie": "SESSDATA=fixture;", "create_only": true,
	})
	if err != nil {
		t.Fatalf("duplicate upsert: %v", err)
	}
	if duplicate["error"] != accountErrorExists {
		t.Fatalf("duplicate upsert result = %#v", duplicate)
	}

	validated, err := handler.accountValidate(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "primary",
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	account, ok := validated["account"].(Account)
	if !ok || account.CredentialState != CredentialValid || account.UID != "123456" || account.Nickname != "测试账号" {
		t.Fatalf("validate result = %#v", validated)
	}
	if state, exists := actions.AccountCredentialState("bilibili", "primary"); !exists || state != CredentialValid {
		t.Fatalf("persisted credential state = %q, exists=%v", state, exists)
	}

	listed, err := handler.accountList(context.Background(), actions, map[string]any{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	items, ok := listed["accounts"].([]Account)
	if !ok || len(items) != 1 || items[0].AccountID != "primary" {
		t.Fatalf("list result = %#v", listed)
	}

	deleted, err := handler.accountDelete(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "primary",
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted["deleted"] != true {
		t.Fatalf("delete result = %#v", deleted)
	}
	if _, exists := actions.Secrets["account.bilibili.primary.cookie"]; exists {
		t.Fatal("cookie secret was not deleted")
	}
	if state, exists := actions.AccountCredentialState("bilibili", "primary"); exists {
		t.Fatalf("account record survived delete: %q", state)
	}
}

func TestAccountQRLoginPersistsAccount(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, accountTestPlatform(func(string) AccountValidation {
		return AccountValidation{State: CredentialValid}
	}))

	created, err := handler.accountQRCreate(context.Background(), actions, map[string]any{"platform": "bilibili"}, Settings{})
	if err != nil {
		t.Fatalf("qr create: %v", err)
	}
	loginID, ok := created["login_id"].(string)
	if !ok || loginID == "" || created["qrcode_url"] != "https://example.test/qr" {
		t.Fatalf("qr create result = %#v", created)
	}

	polled, err := handler.accountQRPoll(context.Background(), actions, map[string]any{
		"platform": "bilibili", "login_id": loginID,
	})
	if err != nil {
		t.Fatalf("qr poll: %v", err)
	}
	if polled["state"] != QRLoginStateSucceeded {
		t.Fatalf("qr poll result = %#v", polled)
	}
	saved, ok := polled["account"].(*Account)
	if !ok || saved.UID != "qr-user" || saved.Nickname != "扫码账号" || saved.CredentialState != CredentialValid {
		t.Fatalf("qr saved account = %#v", polled["account"])
	}
	if actions.Secrets["account.bilibili.qr-user.cookie"] != "SESSDATA=fixture;" {
		t.Fatalf("qr cookie was not stored: %#v", actions.Secrets)
	}
}

func TestAccountValidationRejectsInvalidID(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, accountTestPlatform(func(string) AccountValidation {
		return AccountValidation{State: CredentialValid}
	}))
	result, err := handler.accountUpsert(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "Bad ID", "label": "x", "enabled": true, "cookie": "cookie",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if result["error"] != accountErrorInvalid {
		t.Fatalf("invalid account id result = %#v", result)
	}
}

func TestAccountCheckDueAccountsRespectsInterval(t *testing.T) {
	actions := testkit.NewActions()
	validations := 0
	platform := accountTestPlatform(func(string) AccountValidation {
		validations++
		return AccountValidation{State: CredentialValid, Profile: AccountProfile{UID: "123456", Nickname: "测试账号"}}
	})
	handler := newWorkflowHandler(t, platform)
	if _, err := handler.accountUpsert(context.Background(), actions, map[string]any{
		"platform": "bilibili", "account_id": "primary", "label": "主账号", "enabled": true, "cookie": "SESSDATA=fixture;",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	checked, err := handler.checkDueAccounts(context.Background(), actions, 360)
	if err != nil || checked != 1 || validations != 1 {
		t.Fatalf("first check = %d validations=%d err=%v", checked, validations, err)
	}
	checked, err = handler.checkDueAccounts(context.Background(), actions, 360)
	if err != nil || checked != 0 || validations != 1 {
		t.Fatalf("second check = %d validations=%d err=%v", checked, validations, err)
	}
	checked, err = handler.checkDueAccounts(context.Background(), actions, 0)
	if err != nil || checked != 0 {
		t.Fatalf("disabled interval check = %d err=%v", checked, err)
	}
}
