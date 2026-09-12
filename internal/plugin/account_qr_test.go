package plugin

import (
	"context"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleQRProvider struct {
	expires time.Duration
	entered chan struct{}
	closed  chan struct{}
	closes  atomic.Int32
	future  bool
}

func (p *lifecycleQRProvider) Create(_ context.Context, _ SourceActions, now time.Time) (QRLoginSession, error) {
	return QRLoginSession{Token: "fixture", QRCodeURL: "https://example.test/scan", State: QRLoginStatePendingScan, ExpiresAt: now.Add(p.expires)}, nil
}
func (p *lifecycleQRProvider) Poll(ctx context.Context, _ SourceActions, s QRLoginSession, _ time.Time) (QRLoginSession, error) {
	if p.future {
		s.State = "future-state"
		return s, nil
	}
	close(p.entered)
	<-ctx.Done()
	return s, ctx.Err()
}
func (p *lifecycleQRProvider) Close(context.Context, SourceActions, QRLoginSession) {
	if p.closes.Add(1) == 1 {
		close(p.closed)
	}
}

func TestQRLoginExpiresWithoutPolling(t *testing.T) {
	provider := &lifecycleQRProvider{expires: 30 * time.Millisecond, closed: make(chan struct{})}
	manager := NewQRLoginManager(map[string]QRLoginProviderFactory{"fixture": func(QRLoginOptions) QRLoginProvider { return provider }}, time.Now, nil)
	defer manager.Close()
	actions := testkit.NewActions()
	created, err := manager.Create(context.Background(), actions, "fixture", QRLoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.closed:
	case <-time.After(time.Second):
		t.Fatal("expired provider did not close")
	}
	result, err := manager.Poll(context.Background(), actions, "fixture", created.LoginID)
	if err != nil || result.State != QRLoginStateExpired {
		t.Fatalf("expired result: %#v %v", result, err)
	}
	manager.Close()
	if provider.closes.Load() != 1 {
		t.Fatal("provider was closed more than once")
	}
}

func TestQRLoginCancelInterruptsPoll(t *testing.T) {
	provider := &lifecycleQRProvider{expires: time.Minute, entered: make(chan struct{}), closed: make(chan struct{})}
	manager := NewQRLoginManager(map[string]QRLoginProviderFactory{"fixture": func(QRLoginOptions) QRLoginProvider { return provider }}, time.Now, nil)
	defer manager.Close()
	actions := testkit.NewActions()
	created, err := manager.Create(context.Background(), actions, "fixture", QRLoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _, _ = manager.Poll(context.Background(), actions, "fixture", created.LoginID); close(done) }()
	<-provider.entered
	if err := manager.Cancel(context.Background(), actions, "fixture", created.LoginID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not release poll")
	}
}

func TestQRLoginUnknownStateFailsClosed(t *testing.T) {
	provider := &lifecycleQRProvider{expires: time.Minute, closed: make(chan struct{}), future: true}
	manager := NewQRLoginManager(map[string]QRLoginProviderFactory{"fixture": func(QRLoginOptions) QRLoginProvider { return provider }}, time.Now, nil)
	defer manager.Close()
	actions := testkit.NewActions()
	created, err := manager.Create(context.Background(), actions, "fixture", QRLoginOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Poll(context.Background(), actions, "fixture", created.LoginID)
	if err != nil || result.State != QRLoginStateFailed {
		t.Fatalf("future state: %#v %v", result, err)
	}
}
