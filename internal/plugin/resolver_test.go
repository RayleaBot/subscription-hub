package plugin

import (
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
