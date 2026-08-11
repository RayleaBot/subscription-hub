package plugin

import (
	"bytes"
	"html/template"
	"os"
	"strings"
	"testing"
)

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
