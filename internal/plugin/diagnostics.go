package plugin

import (
	"fmt"
	"regexp"
	"strings"
)

func failureText(platform, stage, message string) string {
	return fmt.Sprintf("[%s/%s] %s", platform, stage, DiagnosticExcerpt(message, 500))
}

var credentialPattern = regexp.MustCompile(`(?i)(["']?)(SESSDATA|bili_jct|DedeUserID(?:__ckMd5)?|sid|buvid3|buvid4|ac_time_value|cookie|access[_-]?token|refresh[_-]?token)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^;,\s]+)`)

var authorizationPattern = regexp.MustCompile(`(?i)(["']?)(authorization)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|(?:bearer|basic)\s+[^;,\s]+|[^;,\s]+)`)

var douyinSecretPattern = regexp.MustCompile(`(?i)(["']?)(msToken|ttwid|s_v_web_id|webid|odin_tt|passport_csrf_token)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^;,\s]+)`)

func DiagnosticExcerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	value = authorizationPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = credentialPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = weiboSecretPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = douyinSecretPattern.ReplaceAllString(value, "${1}${2}${3}${4}[已隐藏]")
	value = strings.TrimRight(strings.TrimSpace(value), "。.;； ")
	if len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

var weiboSecretPattern = regexp.MustCompile(`(?i)(["']?)(SUBP?|XSRF-TOKEN|X-CSRF-TOKEN|_T_WM|MLOGIN|user_token)(["']?)(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^;,\s]+)`)
