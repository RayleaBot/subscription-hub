package bilibili

import (
	"context"
	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/subscription-hub/internal/plugin"
	"github.com/RayleaBot/subscription-hub/internal/testkit"
	"testing"
)

func TestAccountValidationClassifiesAuthenticationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body, state string
	}{
		{"risk", 200, `{"code":-352}`, plugin.CredentialUnknown},
		{"blocked", 200, `{"code":-412}`, plugin.CredentialUnknown},
		{"upstream", 503, `{}`, plugin.CredentialUnknown},
		{"missing login state", 200, `{"code":0}`, plugin.CredentialUnknown},
		{"expired", 200, `{"code":-101}`, plugin.CredentialInvalid},
		{"not logged in", 200, `{"code":0,"data":{"isLogin":false}}`, plugin.CredentialInvalid},
		{"unauthorized", 401, `{}`, plugin.CredentialInvalid},
		{"valid", 200, `{"code":0,"data":{"isLogin":true,"mid":123,"uname":"fixture"}}`, plugin.CredentialValid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testkit.NewActions()
			a.HTTPDefault = rayleabot.ActionResult{"status_code": tc.status, "body_text": tc.body}
			if result := ValidateAccount(context.Background(), a, "SESSDATA=fixture;"); result.State != tc.state {
				t.Fatalf("state=%s, want %s", result.State, tc.state)
			}
		})
	}
}
