package plugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

const BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"

func CookieField(cookie, name string) string {
	for _, part := range strings.Split(cookie, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.TrimSpace(key) == name {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func DecodeHTTPDocument(result rayleabot.ActionResult) map[string]any {
	body := StringScalar(result["body_text"])
	if body == "" {
		if encoded := StringScalar(result["body_base64"]); encoded != "" {
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err == nil && utf8.Valid(raw) {
				body = string(raw)
			}
		}
	}
	if body == "" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil
	}
	return document
}

func IsHTTPActionPermissionError(err error) bool {
	var actionErr *rayleabot.ActionError
	if errors.As(err, &actionErr) {
		if actionErr.Code == "plugin.permission_denied" {
			return true
		}
	}
	text := strings.ToLower(fmt.Sprint(err))
	return strings.Contains(text, "permission")
}
