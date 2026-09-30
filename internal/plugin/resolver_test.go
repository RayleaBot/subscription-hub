package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/RayleaBot/plugin-subscription-hub/internal/testkit"
)

func TestResolverImagePlanKeepsHTTPAndProtocolRelativeImages(t *testing.T) {
	plan := ResolverImagePlan(Update{
		"images": []map[string]any{
			{"url": " http://images.example/one.png "},
			{"url": "//images.example/two.png"},
			{"url": "https://images.example/three.png"},
			{"url": "file:///private/image.png"},
			{"url": "data:image/png;base64,fixture"},
		},
		"original": map[string]any{"images": []map[string]any{
			{"url": "http://images.example/one.png"},
			{"url": "", "candidates": []any{"http://images.example/four.png"}},
		}},
	}, nil, "image")
	urls := make([]string, 0, len(plan.Sources))
	for _, source := range plan.Sources {
		urls = append(urls, source.URLs[0])
	}
	want := []string{"http://images.example/one.png", "https://images.example/two.png", "https://images.example/three.png", "http://images.example/four.png"}
	if !reflect.DeepEqual(urls, want) {
		t.Fatalf("image sources = %#v, want %#v", urls, want)
	}
}

type downloadedImageActions struct {
	*testkit.Actions
	paths []string
}

func (actions *downloadedImageActions) Call(ctx context.Context, action string, input, output any) error {
	if action == "message.forward.send" {
		request := input.(map[string]any)
		for index := range MapSliceValue(request["messages"]) {
			path := StringScalar(NestedValue(forwardMedia(request, index), "data", "file"))
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if string(data) != fmt.Sprintf("original-%d", index) {
				return fmt.Errorf("original image %d changed or reordered", index)
			}
			actions.paths = append(actions.paths, path)
		}
	}
	return actions.Actions.Call(ctx, action, input, output)
}

func TestHTTPOriginalImagesAreDownloadedAndForwarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "original-%s", r.URL.Query().Get("index"))
	}))
	defer server.Close()
	images := make([]map[string]any, 8)
	for index := range images {
		images[index] = map[string]any{"url": fmt.Sprintf("%s/image?index=%d", server.URL, index)}
	}
	plan := ResolverImagePlan(Update{"images": images}, nil, "original")
	actions := &downloadedImageActions{Actions: testkit.NewActions()}
	handler := newWorkflowHandler(t)
	err := handler.deliverResolverMedia(t.Context(), actions, "bilibili", plan, ResolverMediaSettings{}, resolverSendTarget{
		TargetType: "group", TargetID: "10001", SubjectName: "图文作者", SenderID: "20002", SenderName: "分享者",
	})
	if err != nil || len(actions.paths) != 8 || len(actions.ForwardRequests) != 1 || len(actions.Messages) != 0 {
		t.Fatalf("original media delivery: images=%d forwards=%d direct=%d error=%v", len(actions.paths), len(actions.ForwardRequests), len(actions.Messages), err)
	}
	request := actions.ForwardRequests[0]
	if request["source"] != "图文作者" || forwardNode(request, 0)["uin"] != "20002" {
		t.Fatalf("forward identity = %#v", request)
	}
	for _, path := range actions.paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("confirmed media was not cleaned up: %v", err)
		}
	}
}

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
