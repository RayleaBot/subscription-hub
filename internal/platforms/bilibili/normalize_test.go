package bilibili

import (
	"testing"
)

func TestBilibiliDynamicServiceMapping(t *testing.T) {
	for input, expected := range map[string]string{
		"DYNAMIC_TYPE_AV": "video", "DYNAMIC_TYPE_ARTICLE": "article", "DYNAMIC_TYPE_FORWARD": "repost", "DYNAMIC_TYPE_DRAW": "image_text",
	} {
		if got := bilibiliDynamicService(input); got != expected {
			t.Fatalf("service for %s = %s", input, got)
		}
	}
}

func TestBilibiliArticleUsesColumnLabel(t *testing.T) {
	if got := New().Services.Label("article"); got != "专栏" {
		t.Fatalf("catalog article label = %q, want 专栏", got)
	}
	if got := bilibiliServiceLabel("article"); got != "专栏" {
		t.Fatalf("render article label = %q, want 专栏", got)
	}
	if got := dynamicCategory("article"); got != "专栏动态" {
		t.Fatalf("article category = %q, want 专栏动态", got)
	}
}
