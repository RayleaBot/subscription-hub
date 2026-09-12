package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestAccountHTTPPreservesSeparateCookies(t *testing.T) {
	actions := testkit.NewActions()
	actions.HTTPDefault = rayleabot.ActionResult{"status_code": 200, "headers": map[string]any{}, "set_cookies": []any{"visitor=fixture; Expires=Wed, 09 Jun 2032 10:18:14 GMT; Path=/", "SUB=fixture; HttpOnly; Path=/", "SUBP=fixture; Path=/"}, "body_text": "{}"}
	request, _ := http.NewRequest("GET", "https://weibo.com/", nil)
	response, err := NewAccountHTTPClient(actions).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	cookies := response.Cookies()
	if len(cookies) != 3 || cookies[1].Name != "SUB" || cookies[0].Expires.IsZero() {
		t.Fatalf("parsed cookies: %#v", cookies)
	}
}

func TestUncertainValidationPreservesProfile(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, accountTestPlatform(func(string) AccountValidation {
		return AccountValidation{State: CredentialUnknown, Message: "temporary failure"}
	}))
	before := Account{Platform: "bilibili", AccountID: "primary", Enabled: true, UID: "123", Nickname: "existing", AvatarURL: "https://i0.hdslb.com/bfs/face/fixture.jpg"}
	if _, err := SaveAccount(context.Background(), actions, before, "SESSDATA=fixture;", workflowNow); err != nil {
		t.Fatal(err)
	}
	if _, err := handler.accountValidate(context.Background(), actions, map[string]any{"platform": "bilibili", "account_id": "primary"}); err != nil {
		t.Fatal(err)
	}
	after, _, _ := GetAccount(context.Background(), actions, "bilibili", "primary")
	if after.UID != before.UID || after.Nickname != before.Nickname || after.AvatarURL != before.AvatarURL || after.CredentialState != CredentialUnknown {
		t.Fatalf("failed check changed profile: %#v", after)
	}
}

func TestInFlightValidationCannotOverwriteDeletionOrEdit(t *testing.T) {
	for _, mutation := range []string{"delete", "replace"} {
		t.Run(mutation, func(t *testing.T) {
			actions := testkit.NewActions()
			entered, proceed := make(chan struct{}), make(chan struct{})
			handler := newWorkflowHandler(t, accountTestPlatform(func(string) AccountValidation {
				close(entered)
				<-proceed
				return AccountValidation{State: CredentialInvalid}
			}))
			payload := map[string]any{"platform": "bilibili", "account_id": "primary", "label": "old", "cookie": "SESSDATA=fixture;"}
			if _, err := handler.accountUpsert(context.Background(), actions, payload); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := handler.accountValidate(context.Background(), actions, payload); done <- err }()
			<-entered
			if mutation == "delete" {
				if err := DeleteAccount(context.Background(), actions, "bilibili", "primary"); err != nil {
					t.Fatal(err)
				}
			} else {
				payload = map[string]any{"platform": "bilibili", "account_id": "primary", "label": "new", "enabled": false, "cookie": "SESSDATA=replacement;"}
				if _, err := handler.accountUpsert(context.Background(), actions, payload); err != nil {
					t.Fatal(err)
				}
			}
			close(proceed)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			saved, exists, err := GetAccount(context.Background(), actions, "bilibili", "primary")
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "delete" {
				if exists {
					t.Fatal("old check recreated deleted account")
				}
				return
			}
			if !exists || saved.Label != "new" || saved.Enabled || saved.CredentialState != CredentialUnknown {
				t.Fatalf("old check overwrote edit in same clock tick: %#v", saved)
			}
		})
	}
}

type failingAccountMetadata struct{ *testkit.Actions }

func (a failingAccountMetadata) KVSet(context.Context, string, any) (rayleabot.ActionResult, error) {
	return nil, errors.New("metadata unavailable")
}

func TestCookieReplacementRollsBackWhenMetadataFails(t *testing.T) {
	actions := testkit.NewActions()
	account := Account{Platform: "bilibili", AccountID: "primary", Enabled: true}
	if _, err := SaveAccount(context.Background(), actions, account, "SESSDATA=fixture;", workflowNow); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAccount(context.Background(), failingAccountMetadata{actions}, account, "SESSDATA=replacement;", workflowNow); err == nil {
		t.Fatal("metadata failure was ignored")
	}
	cookie, err := AccountCookie(context.Background(), actions, account)
	if err != nil || cookie != "SESSDATA=fixture;" {
		t.Fatal("failed save replaced credential")
	}
}

func TestAccountListFiltersAndPaginates(t *testing.T) {
	actions := testkit.NewActions()
	handler := newWorkflowHandler(t, accountTestPlatform(func(string) AccountValidation { return AccountValidation{} }))
	for i := 0; i < 23; i++ {
		_, err := SaveAccount(context.Background(), actions, Account{Platform: "bilibili", AccountID: fmt.Sprintf("account-%02d", i), Label: "target", Enabled: true}, "SESSDATA=fixture;", workflowNow)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := handler.accountList(context.Background(), actions, map[string]any{"query": "target", "offset": 20, "limit": 20})
	if err != nil {
		t.Fatal(err)
	}
	items := result["accounts"].([]Account)
	if result["total"] != 23 || len(items) != 3 || items[0].AccountID != "account-20" {
		t.Fatalf("page: %#v", result)
	}
	result, err = handler.accountList(context.Background(), actions, map[string]any{"query": "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if result["total"] != 0 || len(result["accounts"].([]Account)) != 0 {
		t.Fatal("empty query result is not empty")
	}
}

func TestObservedFailureCannotInvalidateReplacementCookie(t *testing.T) {
	actions := testkit.NewActions()
	account := Account{Platform: "weibo", AccountID: "primary", Enabled: true}
	if _, err := SaveAccount(context.Background(), actions, account, "SUB=replacement;", time.Now()); err != nil {
		t.Fatal(err)
	}
	ObserveAccountFailure(context.Background(), actions, "weibo", "primary", "SUB=old;", ErrorAuth)
	saved, _, _ := GetAccount(context.Background(), actions, "weibo", "primary")
	if saved.CredentialState != CredentialUnknown {
		t.Fatal("old response invalidated new credential")
	}
}
