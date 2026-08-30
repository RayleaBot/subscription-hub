package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestResolverMediaPlanMessageDistinguishesLimitsFromFailures(t *testing.T) {
	t.Run("duration limit", func(t *testing.T) {
		message := resolverMediaPlanMessage(&ResolverMediaSkippedError{Reason: "视频时长 768 秒，超过超级管理员设置的 480 秒限制，不发送视频"})
		if message != "视频时长 768 秒，超过超级管理员设置的 480 秒限制，不发送视频" {
			t.Fatalf("unexpected limit message: %q", message)
		}
	})

	t.Run("planning failure", func(t *testing.T) {
		message := resolverMediaPlanMessage(errors.New("没有获取到视频地址"))
		if message != "媒体解析失败：没有获取到视频地址" {
			t.Fatalf("unexpected failure message: %q", message)
		}
	})
}

func TestExpandResolverURLLeavesDouyinShortLinksToPlatform(t *testing.T) {
	const rawURL = "https://v.douyin.com/4ZDtWeIBr4g/"
	if got := expandResolverURL(context.Background(), rawURL); got != rawURL {
		t.Fatalf("expandResolverURL() = %q, want platform-owned URL", got)
	}
}

func TestResolverTemplateIDUsesResolverSpecificCards(t *testing.T) {
	for platform, want := range map[string]string{
		"bilibili": "bilibili-resolver",
		"weibo":    "weibo-resolver",
		"douyin":   "douyin-resolver",
	} {
		if got := resolverTemplateID(platform); got != want {
			t.Fatalf("resolverTemplateID(%q) = %q, want %q", platform, got, want)
		}
	}
}
