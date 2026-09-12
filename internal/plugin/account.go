package plugin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CredentialUnknown = "unknown"
	CredentialValid   = "valid"
	CredentialInvalid = "invalid"

	accountKVKeyPrefix = "account:"
)

var (
	accountStoreMu     sync.Mutex
	accountIDPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9_.-]{0,62}[a-z0-9])?$`)
	ErrAccountExists   = errors.New("account already exists")
	ErrAccountInvalid  = errors.New("account is invalid")
	ErrAccountNotFound = errors.New("account not found")
)

// Account is the plugin-owned metadata for one platform login. The cookie
// itself lives in the plugin secret namespace, never in this record.
type Account struct {
	Revision            string `json:"revision"`
	Platform            string `json:"platform"`
	AccountID           string `json:"account_id"`
	Label               string `json:"label"`
	Enabled             bool   `json:"enabled"`
	UID                 string `json:"uid,omitempty"`
	UniqueID            string `json:"unique_id,omitempty"`
	Nickname            string `json:"nickname,omitempty"`
	AvatarURL           string `json:"avatar_url,omitempty"`
	CredentialState     string `json:"credential_state"`
	CredentialCheckedAt string `json:"credential_checked_at,omitempty"`
	CredentialLastError string `json:"credential_last_error,omitempty"`
	UpdatedAt           string `json:"updated_at"`
	// Cookie is only populated for in-process consumers and is never stored in
	// the account record.
	Cookie string `json:"-"`
}

// AccountProfile is the platform-visible identity discovered from a cookie.
type AccountProfile struct {
	UID       string
	UniqueID  string
	Nickname  string
	AvatarURL string
}

func (profile AccountProfile) Empty() bool {
	return strings.TrimSpace(profile.UID) == "" &&
		strings.TrimSpace(profile.Nickname) == "" &&
		strings.TrimSpace(profile.AvatarURL) == ""
}

// MergeAccountProfiles fills missing fields of base from next.
func MergeAccountProfiles(base, next AccountProfile) AccountProfile {
	if strings.TrimSpace(base.UID) == "" {
		base.UID = strings.TrimSpace(next.UID)
	}
	if strings.TrimSpace(base.Nickname) == "" {
		base.Nickname = strings.TrimSpace(next.Nickname)
	}
	if strings.TrimSpace(base.AvatarURL) == "" {
		base.AvatarURL = strings.TrimSpace(next.AvatarURL)
	}
	return base
}

// JSONStringValue converts the common JSON scalar shapes to a string.
func JSONStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return StringScalar(value)
	}
}

// AccountHTTPClientFollowTransport builds a redirect-following client over an
// existing transport so profile probes can reuse the action transport.
func AccountHTTPClientFollowTransport(transport http.RoundTripper) *http.Client {
	if transport == nil {
		return NewAccountHTTPClientFollow(nil)
	}
	return &http.Client{Transport: transport, Timeout: accountRequestTimeout * time.Second}
}

// AccountValidation is the result of one credential check performed by a
// platform adapter. State is valid, invalid (auth rejected or expired), or
// unknown (transport, rate limit, or risk-control uncertainty).
type AccountValidation struct {
	State   string
	Message string
	Profile AccountProfile
}

func AccountKVKey(platform, accountID string) string {
	return accountKVKeyPrefix + strings.TrimSpace(platform) + ":" + strings.TrimSpace(accountID)
}

func AccountSecretKey(platform, accountID string) string {
	return "account." + strings.TrimSpace(platform) + "." + strings.TrimSpace(accountID) + ".cookie"
}

func NormalizeAccountID(value string) string {
	return strings.TrimSpace(value)
}

func ValidAccountID(value string) bool {
	return accountIDPattern.MatchString(strings.TrimSpace(value))
}

func NormalizeCredentialState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CredentialValid:
		return CredentialValid
	case CredentialInvalid:
		return CredentialInvalid
	default:
		return CredentialUnknown
	}
}

func (account Account) Key() string {
	return account.Platform + ":" + account.AccountID
}

func (account Account) Configured() bool {
	return strings.TrimSpace(account.UID) != "" || strings.TrimSpace(account.Nickname) != ""
}

func (account *Account) normalize(now time.Time) {
	account.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	account.AccountID = NormalizeAccountID(account.AccountID)
	account.Label = strings.TrimSpace(account.Label)
	account.UID = strings.TrimSpace(account.UID)
	account.Nickname = strings.TrimSpace(account.Nickname)
	account.AvatarURL = strings.TrimSpace(account.AvatarURL)
	account.CredentialState = NormalizeCredentialState(account.CredentialState)
	account.CredentialCheckedAt = strings.TrimSpace(account.CredentialCheckedAt)
	account.CredentialLastError = strings.TrimSpace(account.CredentialLastError)
	account.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	account.Revision = rand.Text()
	if account.Label == "" {
		account.Label = account.AccountID
	}
}

func accountFromValue(value any) (Account, bool) {
	object := MapValue(value)
	if object == nil {
		if typed, ok := value.(Account); ok {
			return typed, true
		}
		return Account{}, false
	}
	raw, err := json.Marshal(object)
	if err != nil {
		return Account{}, false
	}
	var account Account
	if err := json.Unmarshal(raw, &account); err != nil {
		return Account{}, false
	}
	return account, true
}

func ListAccounts(ctx context.Context, actions SourceActions, platform string) ([]Account, error) {
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	prefix := accountKVKeyPrefix
	if platform = strings.ToLower(strings.TrimSpace(platform)); platform != "" {
		prefix += platform + ":"
	}
	result, err := actions.KVList(ctx, prefix)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0)
	for _, raw := range SliceValue(result["keys"]) {
		key := StringScalar(raw)
		if key == "" {
			continue
		}
		stored, err := actions.KVGet(ctx, key)
		if err != nil {
			return nil, err
		}
		value, exists := ActionStoredValue(stored)
		if !exists {
			continue
		}
		account, ok := accountFromValue(value)
		if !ok {
			continue
		}
		account.normalizeKey()
		accounts = append(accounts, account)
	}
	slices.SortFunc(accounts, func(left, right Account) int {
		if left.Platform != right.Platform {
			return strings.Compare(left.Platform, right.Platform)
		}
		if left.UpdatedAt != right.UpdatedAt {
			return strings.Compare(left.UpdatedAt, right.UpdatedAt)
		}
		return strings.Compare(left.AccountID, right.AccountID)
	})
	return accounts, nil
}

func (account *Account) normalizeKey() {
	account.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	account.AccountID = NormalizeAccountID(account.AccountID)
}

func GetAccount(ctx context.Context, actions SourceActions, platform, accountID string) (Account, bool, error) {
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	return getAccount(ctx, actions, platform, accountID)
}

func getAccount(ctx context.Context, actions SourceActions, platform, accountID string) (Account, bool, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountID = NormalizeAccountID(accountID)
	if platform == "" || accountID == "" {
		return Account{}, false, nil
	}
	result, err := actions.KVGet(ctx, AccountKVKey(platform, accountID))
	if err != nil {
		return Account{}, false, err
	}
	value, exists := ActionStoredValue(result)
	if !exists {
		return Account{}, false, nil
	}
	account, ok := accountFromValue(value)
	if !ok {
		return Account{}, false, nil
	}
	account.normalizeKey()
	return account, true, nil
}

// SaveAccount writes metadata and optionally replaces the stored cookie.
// An empty cookie keeps the existing secret.
func SaveAccount(ctx context.Context, actions RuntimeActions, account Account, cookie string, now time.Time) (Account, error) {
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	if strings.TrimSpace(cookie) != "" {
		account.CredentialState, account.CredentialCheckedAt, account.CredentialLastError = CredentialUnknown, "", ""
	}
	return saveAccount(ctx, actions, account, cookie, now)
}

// The metadata and secret actions share the same process lock. If the second
// store fails, restore the first so a failed save cannot replace a credential.
func saveAccount(ctx context.Context, actions RuntimeActions, account Account, cookie string, now time.Time) (Account, error) {
	account.normalize(now)
	if account.Platform == "" || !ValidAccountID(account.AccountID) {
		return Account{}, ErrAccountInvalid
	}
	var previous string
	var err error
	if strings.TrimSpace(cookie) != "" {
		previous, err = AccountCookie(ctx, actions, account)
		if err != nil {
			return Account{}, err
		}
		if _, err = actions.SecretWrite(ctx, map[string]string{AccountSecretKey(account.Platform, account.AccountID): cookie}); err != nil {
			return Account{}, err
		}
	}
	if _, err = actions.KVSet(ctx, AccountKVKey(account.Platform, account.AccountID), account); err != nil {
		if strings.TrimSpace(cookie) != "" {
			err = errors.Join(err, restoreAccountCookie(ctx, actions, account, previous))
		}
		return Account{}, err
	}
	return account, nil
}

func restoreAccountCookie(ctx context.Context, actions RuntimeActions, account Account, cookie string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	key := AccountSecretKey(account.Platform, account.AccountID)
	var err error
	if cookie == "" {
		_, err = actions.SecretDelete(rollbackCtx, []string{key})
	} else {
		_, err = actions.SecretWrite(rollbackCtx, map[string]string{key: cookie})
	}
	return err
}

func DeleteAccount(ctx context.Context, actions RuntimeActions, platform, accountID string) error {
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	platform, accountID = strings.ToLower(strings.TrimSpace(platform)), NormalizeAccountID(accountID)
	if platform == "" || !ValidAccountID(accountID) {
		return ErrAccountInvalid
	}
	account := Account{Platform: platform, AccountID: accountID}
	previous, err := AccountCookie(ctx, actions, account)
	if err != nil {
		return err
	}
	if _, err = actions.SecretDelete(ctx, []string{AccountSecretKey(platform, accountID)}); err != nil {
		return err
	}
	if _, err = actions.KVDelete(ctx, AccountKVKey(platform, accountID)); err != nil {
		return errors.Join(err, restoreAccountCookie(ctx, actions, account, previous))
	}
	return nil
}

func AccountCookie(ctx context.Context, actions SourceActions, account Account) (string, error) {
	result, err := actions.SecretRead(ctx, AccountSecretKey(account.Platform, account.AccountID))
	if err != nil {
		return "", err
	}
	value, exists := ActionStoredValue(result)
	if !exists {
		return "", nil
	}
	return StringScalar(value), nil
}

// EnabledAccountCookies returns every enabled account with a stored cookie for
// one platform. Accounts without a cookie are skipped.
func EnabledAccountCookies(ctx context.Context, actions SourceActions, platform string) ([]Account, error) {
	accounts, err := ListAccounts(ctx, actions, platform)
	if err != nil {
		return nil, err
	}
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled || account.CredentialState == CredentialInvalid {
			continue
		}
		cookie, err := AccountCookie(ctx, actions, account)
		if err != nil {
			return nil, err
		}
		if cookie == "" {
			continue
		}
		account.Cookie = cookie
		result = append(result, account)
	}
	return result, nil
}

// ObserveAccountFailure only applies to the credential that produced the
// rejection. A delayed source response must not invalidate a replacement.
func ObserveAccountFailure(ctx context.Context, actions SourceActions, platform, accountID, cookie string, kind ErrorKind) {
	if actions == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	current, exists, err := getAccount(ctx, actions, platform, accountID)
	if err != nil || !exists {
		return
	}
	stored, err := AccountCookie(ctx, actions, current)
	if err != nil || stored != cookie {
		return
	}
	state, message := CheckedCredential(platform, kind)
	current.CredentialState = state
	current.CredentialLastError = message
	current.CredentialCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	current.normalize(time.Now())
	_, _ = actions.KVSet(ctx, AccountKVKey(platform, accountID), current)
}

func UpdateCredentialStatus(ctx context.Context, actions SourceActions, account Account, state, checkedAt, lastError string) (bool, error) {
	_, applied, err := commitAccountValidation(ctx, actions, account, AccountValidation{State: state, Message: lastError}, checkedAt, time.Now())
	return applied, err
}

func commitAccountValidation(ctx context.Context, actions SourceActions, before Account, validation AccountValidation, checkedAt string, now time.Time) (Account, bool, error) {
	accountStoreMu.Lock()
	defer accountStoreMu.Unlock()
	current, exists, err := getAccount(ctx, actions, before.Platform, before.AccountID)
	if err != nil {
		return Account{}, false, err
	}
	if !exists {
		return Account{}, false, ErrAccountNotFound
	}
	if current.Revision != before.Revision {
		return current, false, nil
	}
	if validation.State == CredentialValid && !validation.Profile.Empty() {
		current = ApplyProfile(current, validation.Profile)
	}
	current.CredentialState = NormalizeCredentialState(validation.State)
	current.CredentialCheckedAt = checkedAt
	current.CredentialLastError = strings.TrimSpace(validation.Message)
	current.normalize(now)
	if _, err := actions.KVSet(ctx, AccountKVKey(current.Platform, current.AccountID), current); err != nil {
		return Account{}, false, err
	}
	return current, true, nil
}

// ApplyProfile fills the discovery fields of a check result.
func ApplyProfile(account Account, profile AccountProfile) Account {
	account.UID = strings.TrimSpace(profile.UID)
	account.UniqueID = strings.TrimSpace(profile.UniqueID)
	account.Nickname = strings.TrimSpace(profile.Nickname)
	account.AvatarURL = strings.TrimSpace(profile.AvatarURL)
	return account
}
