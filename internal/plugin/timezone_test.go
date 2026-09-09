package plugin

import (
	"testing"
	"time"
)

func TestDisplayTimezoneDoesNotChangeDeliveryAge(t *testing.T) {
	const published = int64(1788994920)
	for _, test := range []struct{ zone, display string }{
		{"Asia/Shanghai", "2026年09月10日 07:02"},
		{"America/Los_Angeles", "2026年09月09日 16:02"},
		{"Asia/Kathmandu", "2026年09月10日 04:47"},
	} {
		t.Run(test.zone, func(t *testing.T) {
			location, err := time.LoadLocation(test.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got := FormatTime(location, published, ""); got != test.display {
				t.Fatalf("display = %q", got)
			}
			for _, age := range []time.Duration{90 * time.Second, 30 * time.Minute, 31 * time.Minute} {
				now := time.Unix(published, 0).Add(age).In(location)
				if got := staleSubscriptionUpdate(Update{"pub_ts": published, "service": "video"}, now, 30*time.Minute); got != (age > 30*time.Minute) {
					t.Fatalf("age %s expired=%v", age, got)
				}
			}
		})
	}
}
