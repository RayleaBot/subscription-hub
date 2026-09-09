package plugin

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
)

func CleanText(value any) string {
	if value == nil {
		return ""
	}
	if object := MapValue(value); object != nil {
		for _, key := range []string{"text", "orig_text", "title", "desc", "summary", "content"} {
			if text := CleanText(object[key]); text != "" {
				return text
			}
		}
		for _, key := range []string{"rich_text_nodes", "paragraphs"} {
			if text := CleanText(object[key]); text != "" {
				return text
			}
		}
		return ""
	}
	if values := SliceValue(value); len(values) > 0 {
		parts := make([]string, 0, len(values))
		for _, item := range values {
			if text := CleanText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	text := RawText(value)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func RawText(value any) string {
	text := StringScalar(value)
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\\r\\n", "\n")
	text = strings.ReplaceAll(text, "\\n", "\n")
	text = strings.ReplaceAll(text, "\\t", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func NormalizeMediaURL(value any) string {
	text := strings.TrimSpace(StringScalar(value))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "//") {
		return "https:" + text
	}
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		return text
	}
	return ""
}

func FormatVideoDuration(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remaining := seconds % 60
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remaining)
	}
	return fmt.Sprintf("%d:%02d", minutes, remaining)
}

func FormatTime(location *time.Location, timestamp int64, fallback string) string {
	if timestamp > 0 {
		return time.Unix(timestamp, 0).In(location).Format("2006年01月02日 15:04")
	}
	return strings.TrimSpace(fallback)
}

// ChinaLocation is used for platform date strings that omit their UTC offset.
// Display formatting uses the host location independently of this source zone.
var ChinaLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return location
}()

func TruncateRunes(value string, limit int) string {
	value = CleanText(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func FirstText(values ...any) string {
	for _, value := range values {
		if text := StringScalar(value); text != "" {
			return text
		}
	}
	return ""
}

func FirstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil && StringScalar(value) != "" {
			return value
		}
		if MapValue(value) != nil || SliceValue(value) != nil {
			return value
		}
	}
	return nil
}

func DedupeStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func EnsureSentence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasSuffix(value, "。") || strings.HasSuffix(value, "！") || strings.HasSuffix(value, "？") || strings.HasSuffix(value, ".") {
		return value
	}
	return value + "。"
}

func FormatCount(value int) string {
	if value < 10000 {
		return strconv.Itoa(value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", float64(value)/10000), "0"), ".") + "万"
}

func SortedServices(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
