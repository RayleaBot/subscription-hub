package plugin

import (
	"regexp"
	"strings"
	"unicode"
)

var URLPattern = regexp.MustCompile(`https?://[^\s<>"，。；、！？）》」]+`)
var HTMLTag = regexp.MustCompile(`<[^>]+>`)

func PathParts(value string) []string {
	items := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func SafeSubjectID(value string) string {
	var result []rune
	for _, char := range strings.TrimSpace(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '.' || char == '-' {
			result = append(result, char)
		}
		if len(result) >= 96 {
			break
		}
	}
	return strings.Trim(string(result), "._-")
}

func Digits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return ""
		}
	}
	return value
}
