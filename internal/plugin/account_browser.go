package plugin

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const (
	browserActionTimeout       = 5 * time.Second
	browserNavigationTimeout   = 12 * time.Second
	browserCookieTimeout       = 3 * time.Second
	browserResponseBodyRetries = 4
)

// Browser mode constants mirror the protocol values.
const (
	ModeAuto      = "auto"
	ModeVisible   = "visible"
	ModeHeadless  = "headless"
	ModeRemoteCDP = "remote_cdp"
)

// OverrideBrowserUserAgent aligns the browser UA with the real Chromium
// version so device signals stay consistent with the rendering engine.
func OverrideBrowserUserAgent(ctx context.Context, session *BrowserSession) error {
	var product string
	err := session.RunTimeout(ctx, 8*time.Second, chromedp.ActionFunc(func(runCtx context.Context) error {
		_, versionProduct, _, _, _, err := browser.GetVersion().Do(runCtx)
		if err != nil {
			return err
		}
		product = strings.TrimSpace(versionProduct)
		return nil
	}))
	if err != nil || product == "" {
		return errors.New("browser version product is unavailable")
	}
	product = strings.ReplaceAll(product, "HeadlessChrome", "Chrome")
	userAgent := BrowserUserAgentForProduct(goruntime.GOOS, product)
	return session.RunTimeout(ctx, 8*time.Second, emulation.SetUserAgentOverride(userAgent).WithAcceptLanguage("zh-CN,zh;q=0.9,en;q=0.8"))
}

// BrowserUserAgentForProduct builds a UA string matching a browser product
// such as "Chrome/152.0.7977.42".
func BrowserUserAgentForProduct(goos, product string) string {
	product = strings.TrimSpace(product)
	if product == "" {
		return ""
	}
	switch goos {
	case "darwin":
		return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) " + product + " Safari/537.36"
	case "linux", "freebsd", "openbsd":
		return "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " + product + " Safari/537.36"
	default:
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " + product + " Safari/537.36"
	}
}

// BrowserSession is one host-managed browser attached over CDP. The host owns
// process launch, profile isolation, and shutdown; this handle only talks CDP.
type BrowserSession struct {
	sessionID string

	allocatorCtx context.Context
	browserCtx   context.Context
	tabCtx       context.Context
	cancelAll    context.CancelFunc
	browserLess  context.CancelFunc
	tabCancel    context.CancelFunc

	closed atomic.Bool
}

// BrowserLauncher is the narrow host capability used to start a session.
type BrowserLauncher interface {
	BrowserLaunch(context.Context, rayleabot.BrowserLaunchRequest) (rayleabot.ActionResult, error)
}

// LaunchBrowserSession asks the host for a browser session and attaches to
// its browser-level CDP endpoint.
func LaunchBrowserSession(ctx context.Context, actions SourceActions, request rayleabot.BrowserLaunchRequest) (*BrowserSession, error) {
	if actions == nil {
		return nil, ErrQRLoginBrowserUnavailable
	}
	result, err := actions.BrowserLaunch(ctx, request)
	if err != nil {
		var actionErr *rayleabot.ActionError
		if errors.As(err, &actionErr) {
			switch actionErr.Code {
			case "platform.resource_busy":
				return nil, ErrQRLoginBrowserBusy
			case "platform.resource_missing":
				return nil, ErrQRLoginBrowserUnavailable
			}
		}
		return nil, err
	}
	debuggerURL := StringScalar(result["debugger_url"])
	sessionID := StringScalar(result["session_id"])
	if debuggerURL == "" || sessionID == "" {
		return nil, ErrQRLoginBrowserUnavailable
	}
	allocatorCtx, cancelAllocator := chromedp.NewRemoteAllocator(context.Background(), debuggerURL)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	tabCtx, cancelTab := chromedp.NewContext(browserCtx)
	session := &BrowserSession{
		sessionID:    sessionID,
		allocatorCtx: allocatorCtx,
		browserCtx:   browserCtx,
		tabCtx:       tabCtx,
		cancelAll:    cancelAllocator,
		browserLess:  cancelBrowser,
		tabCancel:    cancelTab,
	}
	// Allocation belongs to the session. Cancelling an action context used for
	// the first Run would also tear down the allocator's WebSocket.
	initCtx, cancelInit := context.WithTimeout(ctx, 15*time.Second)
	defer cancelInit()
	stopRequest := context.AfterFunc(initCtx, cancelTab)
	err = chromedp.Run(tabCtx)
	stopRequest()
	if err != nil || initCtx.Err() != nil {
		session.Close(context.Background(), actions)
		if err == nil {
			err = initCtx.Err()
		}
		return nil, err
	}
	return session, nil
}

func (session *BrowserSession) SessionID() string {
	if session == nil {
		return ""
	}
	return session.sessionID
}

func (session *BrowserSession) Context() context.Context {
	if session == nil {
		return nil
	}
	return session.tabCtx
}

func (session *BrowserSession) Run(ctx context.Context, actions ...chromedp.Action) error {
	if session == nil || session.closed.Load() {
		return errors.New("browser session is closed")
	}
	runCtx, cancel := browserActionContext(session.tabCtx, ctx, browserActionTimeout)
	defer cancel()
	return chromedp.Run(runCtx, actions...)
}

func (session *BrowserSession) RunTimeout(ctx context.Context, timeout time.Duration, actions ...chromedp.Action) error {
	if session == nil || session.closed.Load() {
		return errors.New("browser session is closed")
	}
	runCtx, cancel := browserActionContext(session.tabCtx, ctx, timeout)
	defer cancel()
	return chromedp.Run(runCtx, actions...)
}

func (session *BrowserSession) Navigate(ctx context.Context, rawURL string) error {
	return session.RunTimeout(ctx, browserNavigationTimeout, chromedp.Navigate(rawURL))
}

func (session *BrowserSession) EvaluateString(ctx context.Context, script string, awaitPromise bool, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = browserActionTimeout
	}
	var raw string
	err := session.RunTimeout(ctx, timeout, chromedp.Evaluate(script, &raw, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
		return params.WithAwaitPromise(awaitPromise)
	}))
	return raw, err
}

func (session *BrowserSession) Evaluate(ctx context.Context, script string, target any, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = browserActionTimeout
	}
	return session.RunTimeout(ctx, timeout, chromedp.Evaluate(script, target))
}

func (session *BrowserSession) Cookies(ctx context.Context, urls []string) (map[string]string, error) {
	if session == nil || session.closed.Load() {
		return nil, errors.New("browser session is closed")
	}
	values := map[string]string{}
	runCtx, cancel := browserActionContext(session.tabCtx, ctx, browserCookieTimeout)
	defer cancel()
	err := chromedp.Run(runCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
		cookies, err := network.GetCookies().WithURLs(urls).Do(runCtx)
		if err != nil {
			return err
		}
		for _, cookie := range cookies {
			name := strings.TrimSpace(cookie.Name)
			if name != "" {
				values[name] = cookie.Value
			}
		}
		return nil
	}))
	return values, err
}

// Close detaches the CDP client and asks the host to stop the browser and
// release its profile. It is safe to call more than once.
func (session *BrowserSession) Close(ctx context.Context, actions SourceActions) {
	if session == nil {
		return
	}
	if session.closed.CompareAndSwap(false, true) {
		session.tabCancel()
		session.browserLess()
		session.cancelAll()
	}
	if actions == nil || session.sessionID == "" {
		return
	}
	closeCtx := ctx
	if closeCtx == nil || closeCtx.Err() != nil {
		closeCtx = context.Background()
	}
	closeCtx, cancel := context.WithTimeout(closeCtx, browserActionTimeout)
	defer cancel()
	_, _ = actions.BrowserClose(closeCtx, session.sessionID)
}

func browserActionContext(sessionCtx, requestCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if sessionCtx == nil {
		sessionCtx = context.Background()
	}
	actionCtx, cancel := context.WithTimeout(sessionCtx, timeout)
	if requestCtx != nil {
		stop := context.AfterFunc(requestCtx, cancel)
		return actionCtx, func() {
			stop()
			cancel()
		}
	}
	return actionCtx, cancel
}

// BrowserResponseCapture watches response bodies for URLs classified by kind.
// It keeps only the latest body per kind, bounded by maxBytes.
type BrowserResponseCapture struct {
	ctx      context.Context
	queue    chan browserCapturedResponse
	updated  chan struct{}
	classify func(string) string
	maxBytes int

	mu     sync.Mutex
	bodies map[string][]byte
	seq    map[string]uint64
}

type browserCapturedResponse struct {
	requestID network.RequestID
	kind      string
}

func WatchBrowserResponses(ctx context.Context, classify func(string) string, maxBytes int) *BrowserResponseCapture {
	if maxBytes <= 0 {
		maxBytes = 256 << 10
	}
	capture := &BrowserResponseCapture{
		ctx:      ctx,
		queue:    make(chan browserCapturedResponse, 16),
		updated:  make(chan struct{}, 1),
		classify: classify,
		maxBytes: maxBytes,
		bodies:   map[string][]byte{},
		seq:      map[string]uint64{},
	}
	chromedp.ListenTarget(ctx, func(event any) {
		response, ok := event.(*network.EventResponseReceived)
		if !ok || response.Response == nil {
			return
		}
		kind := capture.classify(strings.TrimSpace(response.Response.URL))
		if kind == "" {
			return
		}
		select {
		case capture.queue <- browserCapturedResponse{requestID: response.RequestID, kind: kind}:
		default:
		}
	})
	go capture.run()
	return capture
}

func (capture *BrowserResponseCapture) run() {
	for {
		select {
		case <-capture.ctx.Done():
			return
		case response := <-capture.queue:
			body, err := capture.responseBody(response.requestID)
			if err != nil || !capture.store(response.kind, body) {
				continue
			}
			select {
			case capture.updated <- struct{}{}:
			default:
			}
		}
	}
}

func (capture *BrowserResponseCapture) responseBody(requestID network.RequestID) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < browserResponseBodyRetries; attempt++ {
		var body []byte
		err := chromedp.Run(capture.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			body, err = network.GetResponseBody(requestID).Do(ctx)
			return err
		}))
		if err == nil {
			return body, nil
		}
		lastErr = err
		select {
		case <-capture.ctx.Done():
			return nil, capture.ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func (capture *BrowserResponseCapture) store(kind string, body []byte) bool {
	if kind == "" || len(body) == 0 || len(body) > capture.maxBytes {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.bodies[kind] = append([]byte(nil), body...)
	capture.seq[kind]++
	return true
}

func (capture *BrowserResponseCapture) Latest(kind string) []byte {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.bodies[kind]...)
}

func (capture *BrowserResponseCapture) LatestWithSequence(kind string) ([]byte, uint64) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return append([]byte(nil), capture.bodies[kind]...), capture.seq[kind]
}

func (capture *BrowserResponseCapture) Updated() <-chan struct{} {
	return capture.updated
}
