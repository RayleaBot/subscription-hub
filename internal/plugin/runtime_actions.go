package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

// pluginActions is the narrow host boundary used by subscription checks and delivery.
// Keeping it local makes the plugin testable without introducing another runtime or HTTP stack.
type pluginActions interface {
	HTTPRequest(context.Context, rayleabot.HTTPRequest) (rayleabot.ActionResult, error)
	ThirdPartyAccountRead(context.Context, rayleabot.ThirdPartyAccountReadRequest) (rayleabot.ActionResult, error)
	KVGet(context.Context, string) (rayleabot.ActionResult, error)
	KVSet(context.Context, string, any) (rayleabot.ActionResult, error)
	KVDelete(context.Context, string) (rayleabot.ActionResult, error)
	LoggerWrite(context.Context, rayleabot.LoggerWriteRequest) (rayleabot.ActionResult, error)
	RenderImage(context.Context, rayleabot.RenderImageRequest) (rayleabot.ActionResult, error)
	MessageSend(context.Context, rayleabot.MessageSendRequest) (rayleabot.ActionResult, error)
	GroupMemberGet(context.Context, string, string) (rayleabot.ActionResult, error)
}

func mapValue(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func sliceValue(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

// mapSliceValue 同时接受 []any 与 []map[string]any（如 buildSubscriberCards 的返回值）。
func mapSliceValue(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object := mapValue(item); object != nil {
				result = append(result, object)
			}
		}
		return result
	}
	return nil
}

func stringScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case float32:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	default:
		return ""
	}
}

func intScalar(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case json.Number:
		value, _ := typed.Int64()
		return value
	case string:
		value, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return value
	default:
		return 0
	}
}

func floatScalar(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		value, _ := typed.Float64()
		return value
	case string:
		value, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return value
	default:
		return 0
	}
}

func boolScalar(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		value, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return value
	default:
		return intScalar(value) != 0
	}
}

func nestedValue(value any, path ...any) any {
	current := value
	for _, part := range path {
		switch key := part.(type) {
		case string:
			object := mapValue(current)
			if object == nil {
				return nil
			}
			current = object[key]
		case int:
			items := sliceValue(current)
			if key < 0 || key >= len(items) {
				return nil
			}
			current = items[key]
		default:
			return nil
		}
	}
	return current
}

func actionStoredValue(result rayleabot.ActionResult) (any, bool) {
	if result == nil {
		return nil, false
	}
	value, exists := result["value"]
	if explicit, ok := result["exists"].(bool); ok && !explicit {
		return nil, false
	}
	return value, exists
}

func cloneJSONMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&cloned); err != nil {
		return nil
	}
	return cloned
}

func requireMap(value any, label string) (map[string]any, error) {
	result := mapValue(value)
	if result == nil {
		return nil, fmt.Errorf("%s 格式不正确", label)
	}
	return result, nil
}
