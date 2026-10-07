package weibo

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/testkit"
)

func TestWeiboSearchTemplatePreservesInlineAvatarOutsideURLContext(t *testing.T) {
	source, err := os.ReadFile(testkit.RepositoryPath(t, "templates/weibo-search-results/template.html"))
	if err != nil {
		t.Fatalf("read weibo search template: %v", err)
	}
	compiled, err := template.New("weibo-search-results").Parse(string(source))
	if err != nil {
		t.Fatalf("parse weibo search template: %v", err)
	}

	const avatar = "data:image/png;base64,fixture"
	data := map[string]any{
		"Stylesheet": template.CSS(""),
		"Theme":      "default",
		"users": []map[string]any{{
			"rank": 1, "name": "示例博主", "avatar": avatar, "verify_text": "微博认证",
		}},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute weibo search template: %v", err)
	}

	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") || !strings.Contains(html, `data-avatar="`+avatar+`"`) {
		t.Fatalf("inline avatar did not survive template escaping: %s", html)
	}
	if !strings.Contains(html, "image.src = source") {
		t.Fatalf("search template did not activate deferred avatar sources: %s", html)
	}
	if !strings.Contains(html, "v-badge") {
		t.Fatalf("search template did not render the verify badge: %s", html)
	}
}

func TestWeiboSearchTemplateUsesBundledDefaultAvatar(t *testing.T) {
	if _, err := os.ReadFile(testkit.RepositoryPath(t, "templates/weibo-search-results/"+weiboSearchFallbackAvatar)); err != nil {
		t.Fatalf("read weibo default avatar: %v", err)
	}
	source, err := os.ReadFile(testkit.RepositoryPath(t, "templates/weibo-search-results/template.html"))
	if err != nil {
		t.Fatalf("read weibo search template: %v", err)
	}
	if !strings.Contains(string(source), weiboSearchFallbackAvatar) {
		t.Fatalf("weibo search template does not reference the bundled default avatar")
	}
}
