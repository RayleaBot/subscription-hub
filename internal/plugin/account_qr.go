package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	QRLoginStatePendingScan          = "pending_scan"
	QRLoginStatePendingConfirm       = "pending_confirm"
	QRLoginStateVerificationRequired = "verification_required"
	QRLoginStateExpired              = "expired"
	QRLoginStateFailed               = "failed"
	QRLoginStateSucceeded            = "succeeded"
	qrLoginPersistTimeout            = 30 * time.Second
)

var (
	ErrQRLoginUnsupportedPlatform = errors.New("unsupported account qrcode login platform")
	ErrQRLoginSessionNotFound     = errors.New("account qrcode login session not found")
	ErrQRLoginCredentialMissing   = errors.New("account qrcode login credential missing")
	ErrQRLoginBrowserUnavailable  = errors.New("account qrcode login browser unavailable")
	ErrQRLoginBrowserBusy         = errors.New("account qrcode login browser profile busy")
)

type QRLoginCreateResult struct {
	Platform  string
	LoginID   string
	QRCodeURL string
	ExpiresAt time.Time
	State     string
}

type QRLoginPollResult struct {
	Platform     string
	LoginID      string
	State        string
	ExpiresAt    time.Time
	Cookie       string
	Account      AccountProfile
	SavedAccount *Account
}

type QRLoginSession struct {
	Platform     string
	LoginID      string
	Token        string
	QRCodeURL    string
	ExpiresAt    time.Time
	State        string
	Cookie       string
	Account      AccountProfile
	SavedAccount *Account
	Values       map[string]string
	Cookies      map[string]string
}

type QRLoginProvider interface {
	Create(context.Context, SourceActions, time.Time) (QRLoginSession, error)
	Poll(context.Context, SourceActions, QRLoginSession, time.Time) (QRLoginSession, error)
}

type QRLoginProviderLoginIDPrefix interface {
	LoginIDPrefix() string
}

type QRLoginProviderSessionCloser interface {
	Close(context.Context, SourceActions, QRLoginSession)
}

// QRLoginOptions carries provider-agnostic browser preferences from the
// current plugin settings.
type QRLoginOptions struct {
	BrowserMode      string
	BrowserRemoteURL string
}

// QRLoginProviderFactory builds one provider instance per login session.
// HTTP-only providers stay stateless; browser providers keep their session
// state between polls.
type QRLoginProviderFactory func(QRLoginOptions) QRLoginProvider

// QRLoginAccountPersister stores a successful QR login and returns the saved
// account summary.
type QRLoginAccountPersister func(context.Context, RuntimeActions, string, string, AccountProfile, time.Time) (Account, error)

type QRLoginManager struct {
	providers map[string]QRLoginProviderFactory
	persist   QRLoginAccountPersister
	now       func() time.Time
	mu        sync.Mutex
	sessions  map[string]*qrLoginEntry
	closed    bool
}

type qrLoginEntry struct {
	mu           sync.Mutex
	platform     string
	provider     QRLoginProvider
	session      QRLoginSession
	closeSession QRLoginSession
	expiresAt    time.Time
	sessionCtx   context.Context
	cancel       context.CancelFunc
	cancelled    atomic.Bool
	closeOnce    sync.Once
	expireTimer  *time.Timer
	removeTimer  *time.Timer
}

func NewQRLoginManager(providers map[string]QRLoginProviderFactory, now func() time.Time, persist QRLoginAccountPersister) *QRLoginManager {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &QRLoginManager{
		providers: providers,
		persist:   persist,
		now:       now,
		sessions:  make(map[string]*qrLoginEntry),
	}
}

func (manager *QRLoginManager) Create(ctx context.Context, actions RuntimeActions, platform string, options QRLoginOptions) (QRLoginCreateResult, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	factory := manager.providers[platform]
	if factory == nil {
		return QRLoginCreateResult{}, ErrQRLoginUnsupportedPlatform
	}
	provider := factory(options)
	if provider == nil {
		return QRLoginCreateResult{}, ErrQRLoginUnsupportedPlatform
	}
	now := manager.now().UTC()
	session, err := provider.Create(ctx, actions, now)
	if err != nil {
		closeProviderSession(ctx, actions, provider, session)
		return QRLoginCreateResult{}, err
	}
	session.Platform = platform
	session.State = NormalizeQRLoginState(session.State)
	if session.State == "" {
		session.State = QRLoginStatePendingScan
	}
	if session.ExpiresAt.IsZero() {
		session.ExpiresAt = now.Add(3 * time.Minute)
	}
	loginID, err := providerLoginID(provider, platform)
	if err != nil {
		closeProviderSession(ctx, actions, provider, session)
		return QRLoginCreateResult{}, err
	}
	session.LoginID = loginID
	sessionCtx, cancel := context.WithCancel(context.Background())
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		cancel()
		closeProviderSession(ctx, actions, provider, session)
		return QRLoginCreateResult{}, ErrQRLoginSessionNotFound
	}
	stale := manager.pruneExpiredLocked(now)
	entry := &qrLoginEntry{
		platform:     platform,
		provider:     provider,
		session:      session,
		closeSession: CloneQRLoginSession(session),
		expiresAt:    session.ExpiresAt,
		sessionCtx:   sessionCtx,
		cancel:       cancel,
	}
	manager.sessions[loginID] = entry
	remaining := max(0, session.ExpiresAt.Sub(manager.now().UTC()))
	entry.expireTimer = time.AfterFunc(remaining, func() { manager.expire(loginID) })
	entry.removeTimer = time.AfterFunc(remaining+5*time.Minute, func() { _ = manager.Cancel(context.Background(), nil, platform, loginID) })
	manager.mu.Unlock()
	manager.closeEntries(context.Background(), actions, stale, true)
	return QRLoginCreateResultFromSession(session), nil
}

func providerLoginID(provider QRLoginProvider, platform string) (string, error) {
	if prefixer, ok := provider.(QRLoginProviderLoginIDPrefix); ok {
		if prefix := strings.TrimSpace(prefixer.LoginIDPrefix()); prefix != "" {
			return RandomQRLoginIDWithPrefix(prefix)
		}
	}
	return RandomQRLoginID(platform)
}

func (manager *QRLoginManager) Poll(ctx context.Context, actions RuntimeActions, platform, loginID string) (QRLoginPollResult, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	loginID = strings.TrimSpace(loginID)
	now := manager.now().UTC()
	manager.mu.Lock()
	entry, ok := manager.sessions[loginID]
	manager.mu.Unlock()
	if !ok {
		return QRLoginPollResult{}, ErrQRLoginSessionNotFound
	}

	entry.mu.Lock()
	if entry.cancelled.Load() {
		entry.mu.Unlock()
		return QRLoginPollResult{}, ErrQRLoginSessionNotFound
	}
	session := entry.session
	if entry.platform != platform {
		entry.mu.Unlock()
		return QRLoginPollResult{}, ErrQRLoginSessionNotFound
	}
	if IsQRLoginTerminalState(session.State) {
		result := QRLoginPollResultFromSession(session)
		entry.mu.Unlock()
		return result, nil
	}
	if now.After(session.ExpiresAt) && session.State != QRLoginStateSucceeded {
		session.State = QRLoginStateExpired
		entry.session = session
		result := QRLoginPollResultFromSession(session)
		entry.mu.Unlock()
		manager.closeEntry(ctx, actions, entry, false)
		return result, nil
	}

	pollCtx, cancelPoll := context.WithCancel(ctx)
	stopSessionCancel := context.AfterFunc(entry.sessionCtx, cancelPoll)
	next, err := entry.provider.Poll(pollCtx, actions, CloneQRLoginSession(session), now)
	stopSessionCancel()
	cancelPoll()
	if entry.cancelled.Load() {
		entry.mu.Unlock()
		return QRLoginPollResult{}, ErrQRLoginSessionNotFound
	}
	if err != nil {
		entry.mu.Unlock()
		return QRLoginPollResult{}, err
	}
	next.Platform = platform
	next.LoginID = loginID
	next.ExpiresAt = session.ExpiresAt
	next.QRCodeURL = session.QRCodeURL
	next.State = NormalizeQRLoginState(next.State)
	if next.State == "" {
		next.State = QRLoginStateFailed
	}
	if next.State == QRLoginStateSucceeded && manager.persist != nil {
		if entry.cancelled.Load() || entry.sessionCtx.Err() != nil {
			entry.mu.Unlock()
			return QRLoginPollResult{}, ErrQRLoginSessionNotFound
		}
		persistCtx, cancelPersist := context.WithTimeout(entry.sessionCtx, qrLoginPersistTimeout)
		account, err := manager.persist(persistCtx, actions, platform, next.Cookie, next.Account, now)
		cancelPersist()
		if entry.cancelled.Load() || entry.sessionCtx.Err() != nil {
			entry.mu.Unlock()
			return QRLoginPollResult{}, ErrQRLoginSessionNotFound
		}
		if err != nil {
			entry.mu.Unlock()
			return QRLoginPollResult{}, err
		}
		next.SavedAccount = &account
	}
	entry.session = next
	result := QRLoginPollResultFromSession(next)
	terminal := IsQRLoginTerminalState(next.State)
	entry.mu.Unlock()
	if terminal {
		manager.closeEntry(ctx, actions, entry, false)
	}
	return result, nil
}

func (manager *QRLoginManager) Cancel(ctx context.Context, actions RuntimeActions, platform, loginID string) error {
	platform = strings.ToLower(strings.TrimSpace(platform))
	loginID = strings.TrimSpace(loginID)
	manager.mu.Lock()
	entry, ok := manager.sessions[loginID]
	if ok && entry.platform == platform {
		delete(manager.sessions, loginID)
	}
	manager.mu.Unlock()
	if !ok || entry.platform != platform {
		return ErrQRLoginSessionNotFound
	}
	manager.closeEntry(ctx, actions, entry, true)
	entry.waitForPollCompletion()
	return nil
}

func (manager *QRLoginManager) Close() {
	manager.mu.Lock()
	manager.closed = true
	entries := make([]*qrLoginEntry, 0, len(manager.sessions))
	for loginID, entry := range manager.sessions {
		delete(manager.sessions, loginID)
		entries = append(entries, entry)
	}
	manager.mu.Unlock()
	manager.closeEntries(context.Background(), nil, entries, true)
}

func (manager *QRLoginManager) expire(loginID string) {
	manager.mu.Lock()
	entry := manager.sessions[loginID]
	manager.mu.Unlock()
	if entry == nil {
		return
	}
	entry.cancel()
	entry.mu.Lock()
	if !IsQRLoginTerminalState(entry.session.State) {
		entry.session.State = QRLoginStateExpired
	}
	entry.mu.Unlock()
	manager.closeEntry(context.Background(), nil, entry, false)
}

func (manager *QRLoginManager) pruneExpiredLocked(now time.Time) []*qrLoginEntry {
	stale := make([]*qrLoginEntry, 0)
	for loginID, entry := range manager.sessions {
		if now.After(entry.expiresAt.Add(5 * time.Minute)) {
			delete(manager.sessions, loginID)
			stale = append(stale, entry)
		}
	}
	return stale
}

func (manager *QRLoginManager) closeEntries(ctx context.Context, actions SourceActions, entries []*qrLoginEntry, cancelled bool) {
	for _, entry := range entries {
		manager.closeEntry(ctx, actions, entry, cancelled)
		if cancelled {
			entry.waitForPollCompletion()
		}
	}
}

func (entry *qrLoginEntry) waitForPollCompletion() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
}

func (manager *QRLoginManager) closeEntry(ctx context.Context, actions SourceActions, entry *qrLoginEntry, cancelled bool) {
	if entry == nil || entry.provider == nil {
		return
	}
	if cancelled {
		entry.cancelled.Store(true)
		if entry.removeTimer != nil {
			entry.removeTimer.Stop()
		}
	}
	if entry.expireTimer != nil {
		entry.expireTimer.Stop()
	}
	entry.cancel()
	entry.closeOnce.Do(func() {
		closeProviderSession(ctx, actions, entry.provider, CloneQRLoginSession(entry.closeSession))
	})
}

func closeProviderSession(ctx context.Context, actions SourceActions, provider QRLoginProvider, session QRLoginSession) {
	if closer, ok := provider.(QRLoginProviderSessionCloser); ok {
		closer.Close(ctx, actions, session)
	}
}

func QRLoginCreateResultFromSession(session QRLoginSession) QRLoginCreateResult {
	return QRLoginCreateResult{
		Platform:  session.Platform,
		LoginID:   session.LoginID,
		QRCodeURL: session.QRCodeURL,
		ExpiresAt: session.ExpiresAt,
		State:     session.State,
	}
}

func QRLoginPollResultFromSession(session QRLoginSession) QRLoginPollResult {
	return QRLoginPollResult{
		Platform:     session.Platform,
		LoginID:      session.LoginID,
		State:        session.State,
		ExpiresAt:    session.ExpiresAt,
		Cookie:       session.Cookie,
		Account:      session.Account,
		SavedAccount: session.SavedAccount,
	}
}

func NormalizeQRLoginState(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case QRLoginStatePendingScan:
		return QRLoginStatePendingScan
	case QRLoginStatePendingConfirm:
		return QRLoginStatePendingConfirm
	case QRLoginStateVerificationRequired:
		return QRLoginStateVerificationRequired
	case QRLoginStateExpired:
		return QRLoginStateExpired
	case QRLoginStateFailed:
		return QRLoginStateFailed
	case QRLoginStateSucceeded:
		return QRLoginStateSucceeded
	default:
		return ""
	}
}

func IsQRLoginTerminalState(value string) bool {
	switch NormalizeQRLoginState(value) {
	case QRLoginStateExpired, QRLoginStateFailed, QRLoginStateSucceeded:
		return true
	default:
		return false
	}
}

func RandomQRLoginID(platform string) (string, error) {
	prefix := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(platform)), "-", "_")
	if prefix == "" {
		prefix = "account"
	}
	return RandomQRLoginIDWithPrefix(prefix + "_qr")
}

func RandomQRLoginIDWithPrefix(prefix string) (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	prefix = strings.ReplaceAll(strings.TrimSpace(strings.ToLower(prefix)), "-", "_")
	if prefix == "" {
		prefix = "account_qr"
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes[:])), nil
}

func CloneQRLoginSession(session QRLoginSession) QRLoginSession {
	session.Values = CloneStringMap(session.Values)
	session.Cookies = CloneStringMap(session.Cookies)
	if session.SavedAccount != nil {
		account := *session.SavedAccount
		session.SavedAccount = &account
	}
	return session
}

func QRLoginAccountID(profile AccountProfile) string {
	if accountID := NormalizeAccountID(profile.UID); ValidAccountID(accountID) {
		return accountID
	}
	return "primary"
}
