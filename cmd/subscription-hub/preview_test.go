package main

import "testing"

func TestParsePreviewInputSelectsWeiboAndBilibili(t *testing.T) {
	tests := []struct {
		input        string
		wantPlatform string
		wantService  string
	}{
		{input: "", wantPlatform: "bilibili", wantService: "video"},
		{input: "视频", wantPlatform: "bilibili", wantService: "video"},
		{input: "转发", wantPlatform: "bilibili", wantService: "repost"},
		{input: "直播", wantPlatform: "bilibili", wantService: "live"},
		{input: "图文", wantPlatform: "bilibili", wantService: "image_text"},
		{input: "动态", wantPlatform: "bilibili", wantService: "image_text"},
		{input: "微博", wantPlatform: "weibo", wantService: "post"},
		{input: "图片", wantPlatform: "weibo", wantService: "image"},
		{input: "IMAGE", wantPlatform: "weibo", wantService: "image"},
		{input: "文字", wantPlatform: "weibo", wantService: "post"},
		{input: "微博 视频", wantPlatform: "weibo", wantService: "video"},
		{input: "微博 转发", wantPlatform: "weibo", wantService: "repost"},
		{input: "微博视频", wantPlatform: "weibo", wantService: "video"},
		{input: "微博图片", wantPlatform: "weibo", wantService: "image"},
		{input: "b站 转发", wantPlatform: "bilibili", wantService: "repost"},
		{input: "b站视频", wantPlatform: "bilibili", wantService: "video"},
		{input: "bilibili图文", wantPlatform: "bilibili", wantService: "video"},
	}
	for _, test := range tests {
		platform, service := newHandler(t).ParsePreviewInput(test.input)
		if platform != test.wantPlatform || service != test.wantService {
			t.Fatalf("parsePreviewInput(%q) = %s %s, want %s %s", test.input, platform, service, test.wantPlatform, test.wantService)
		}
	}
}

func TestParsePreviewInputCoversDouyin(t *testing.T) {
	for input, want := range map[string][2]string{
		"抖音":        {"douyin", "video"},
		"抖音 直播":     {"douyin", "live"},
		"抖音 图文":     {"douyin", "image_text"},
		"douyin 视频": {"douyin", "video"},
	} {
		platform, service := newHandler(t).ParsePreviewInput(input)
		if platform != want[0] || service != want[1] {
			t.Fatalf("parsePreviewInput(%q) = %q %q, want %q %q", input, platform, service, want[0], want[1])
		}
	}
}
