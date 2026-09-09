package weibo

import (
	"testing"
	"time"
)

func TestWeiboPublicationParsingSeparatesSourceAndDisplayTimezones(t *testing.T) {
	const want = int64(1788994920)
	for _, input := range []string{"2026-09-10 07:02:00", "Thu Sep 10 07:02:00 +0800 2026", "Wed Sep 09 16:02:00 -0700 2026"} {
		if got := weiboPubTS(map[string]any{"created_at": input}); got != want {
			t.Fatalf("%q = %d, want %d", input, got, want)
		}
	}
	mblog := weiboTextMblog("timezone", "6000000001", "内容", want)
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	update := normalizeWeiboMblog(location, mblog, 0)
	if update["created_at"] != "2026年09月10日 07:02" || update["pub_ts"] != want {
		t.Fatalf("normalized update = %#v", update)
	}
}
