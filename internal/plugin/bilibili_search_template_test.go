package plugin

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"html/template"
	"os"
	"strings"
	"testing"
)

const bilibiliOfficialDefaultAvatarSHA256 = "a1cc0fa827befd75d9c248a16e7fc0f37fa1501cd65c78c35d86812b4bab595c"

func TestBilibiliSearchTemplatePreservesInlineAvatarOutsideURLContext(t *testing.T) {
	source, err := os.ReadFile("../../templates/bilibili-search-results/template.html")
	if err != nil {
		t.Fatalf("read search template: %v", err)
	}
	compiled, err := template.New("bilibili-search-results").Parse(string(source))
	if err != nil {
		t.Fatalf("parse search template: %v", err)
	}

	const avatar = "data:image/png;base64,fixture"
	data := map[string]any{
		"Stylesheet": template.CSS(""),
		"Theme":      "default",
		"users": []map[string]any{{
			"rank": 1, "name": "示例 UP", "avatar": avatar,
		}},
	}
	var output bytes.Buffer
	if err := compiled.Execute(&output, data); err != nil {
		t.Fatalf("execute search template: %v", err)
	}

	html := output.String()
	if strings.Contains(html, "#ZgotmplZ") || !strings.Contains(html, `data-avatar="`+avatar+`"`) {
		t.Fatalf("inline avatar did not survive template escaping: %s", html)
	}
	if !strings.Contains(html, "image.src = source") {
		t.Fatalf("search template did not activate deferred avatar sources: %s", html)
	}
}

func TestBilibiliSearchTemplateUsesOfficialDefaultAvatar(t *testing.T) {
	asset, err := os.ReadFile("../../templates/bilibili-search-results/assets/bilibili-default-avatar.gif")
	if err != nil {
		t.Fatalf("read Bilibili default avatar: %v", err)
	}
	if hash := fmt.Sprintf("%x", sha256.Sum256(asset)); hash != bilibiliOfficialDefaultAvatarSHA256 {
		t.Fatalf("Bilibili default avatar hash = %s", hash)
	}

	source, err := os.ReadFile("../../templates/bilibili-search-results/template.html")
	if err != nil {
		t.Fatalf("read search template: %v", err)
	}
	html := string(source)
	if !strings.Contains(html, bilibiliSearchFallbackAvatar) || strings.Contains(html, "assets/avatar.svg") {
		t.Fatalf("search template does not use the official default avatar")
	}
}
