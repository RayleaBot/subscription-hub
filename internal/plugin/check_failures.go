package plugin

import (
	"context"
	"errors"
	"sort"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
)

type checkFailures struct {
	messages []string
	causes   map[string]bool
}

func (failures *checkFailures) add(platform, stage, message string, err error) {
	failures.messages = append(failures.messages, failureText(platform, stage, message))
	code := "unspecified"
	var actionErr *rayleabot.ActionError
	switch {
	case errors.As(err, &actionErr) && actionErr != nil:
		code = actionErr.Code
	case errors.Is(err, context.Canceled):
		code = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	}
	failures.addCause(platform, stage, code)
}

func (failures *checkFailures) addCause(platform, stage, code string) {
	if failures.causes == nil {
		failures.causes = make(map[string]bool)
	}
	failures.causes[platform+":"+stage+":"+code] = true
}

func (failures *checkFailures) keys() []string {
	keys := make([]string, 0, len(failures.causes))
	for key := range failures.causes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type checkLogState struct {
	emitted time.Time
	total   int
	pending int
}
