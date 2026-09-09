package bilibili

import "testing"

func TestLiveStartWithoutOffsetUsesPlatformTimezone(t *testing.T) {
	const want = int64(1788994920)
	for _, input := range []any{"2026-09-10 07:02:00", "2026-09-10 07:02", want} {
		if got := liveStartTimestamp(map[string]any{"live_time": input}); got != want {
			t.Fatalf("%v = %d, want %d", input, got, want)
		}
	}
}
