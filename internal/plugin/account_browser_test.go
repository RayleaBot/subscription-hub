package plugin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
	"github.com/chromedp/chromedp"
)

type attachedBrowserActions struct {
	*testkit.Actions
	endpoint string
	launches int
}

func (a *attachedBrowserActions) BrowserLaunch(context.Context, rayleabot.BrowserLaunchRequest) (rayleabot.ActionResult, error) {
	a.launches++
	return rayleabot.ActionResult{"session_id": "fixture", "debugger_url": a.endpoint}, nil
}

func TestBrowserSourceSessionSurvivesIndividualActions(t *testing.T) {
	path := os.Getenv("RAYLEA_TEST_BROWSER_PATH")
	if path == "" {
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if found, err := exec.LookPath(name); err == nil {
				path = found
				break
			}
		}
	}
	if path == "" {
		t.Skip("Chromium is not installed")
	}
	profile := t.TempDir()
	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(path), chromedp.UserDataDir(profile), chromedp.NoSandbox)
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	t.Cleanup(cancelAllocator)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	if err := chromedp.Run(browser); err != nil {
		t.Fatal(err)
	}
	port, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(string(port))
	if len(parts) != 2 {
		t.Fatal("missing debugging endpoint")
	}
	actions := &attachedBrowserActions{Actions: testkit.NewActions(), endpoint: "ws://127.0.0.1:" + parts[0] + parts[1]}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := LaunchBrowserSession(ctx, sourceBoundary(actions), rayleabot.BrowserLaunchRequest{Profile: "login", Mode: ModeHeadless})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(context.Background(), actions)
	if actions.launches != 1 {
		t.Fatal("source boundary did not forward browser launch")
	}
	for _, expression := range []string{"1+1", "2+2"} {
		var value int
		if err := session.Evaluate(ctx, expression, &value, time.Second); err != nil {
			t.Fatalf("action %s: %v", expression, err)
		}
		if value == 0 {
			t.Fatal("browser did not evaluate expression")
		}
	}
	if _, err := session.Cookies(ctx, []string{"https://example.test/"}); err != nil {
		t.Fatalf("cookie read after actions: %v", err)
	}
}
