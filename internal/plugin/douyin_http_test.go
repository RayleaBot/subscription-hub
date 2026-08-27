package plugin

import (
	"testing"
)

// TestStripDouyinAnticheatCookie 回归线上风控：CK 中陈旧的 __ac_nonce 会触发
// 抖音验证中间页（服务端每次页面响应都会重种该值，程序无法维持新鲜值）。
// 会话构建时必须剥离该字段，避免 HTML 回退路径踩中验证页加剧风控。
func TestStripDouyinAnticheatCookie(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips anticheat field between others",
			in:   "sessionid=abc; __ac_nonce=06a8c1c0d00be0b52; ttwid=def; msToken=ghi",
			want: "sessionid=abc; ttwid=def; msToken=ghi",
		},
		{
			name: "strips case-insensitive and empty fields",
			in:   "__AC_NONCE=x;; sessionid=abc;",
			want: "sessionid=abc",
		},
		{
			name: "keeps cookie without anticheat field intact",
			in:   "sessionid=abc; sessionid_ss=def",
			want: "sessionid=abc; sessionid_ss=def",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stripDouyinAnticheatCookie(tc.in); got != tc.want {
				t.Fatalf("stripDouyinAnticheatCookie(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDouyinSearchVerifyCheck 回归线上风控：搜索 API 返回 status_code=0 且空结果，
// 但 search_nil_info.search_nil_type=verify_check 是服务端明确要求人工验证的标记，
// 必须归为 risk_control 立即停止探测，而不是当作普通空结果继续走页面回退。
func TestDouyinSearchVerifyCheck(t *testing.T) {
	t.Parallel()

	if !douyinSearchVerifyCheck(map[string]any{
		"status_code": 0,
		"user_list":   []any{},
		"search_nil_info": map[string]any{
			"search_nil_type": "verify_check",
		},
	}) {
		t.Fatal("verify_check shell must be detected")
	}
	for _, doc := range []map[string]any{
		nil,
		{},
		{"search_nil_info": nil},
		{"search_nil_info": map[string]any{}},
		{"search_nil_info": map[string]any{"search_nil_type": "normal_empty"}},
		{"status_code": 0, "user_list": []any{}},
	} {
		if douyinSearchVerifyCheck(doc) {
			t.Fatalf("unexpected verify_check detection for %#v", doc)
		}
	}
}
