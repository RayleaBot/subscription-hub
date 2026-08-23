package plugin

import (
	"testing"
)

func TestPrepareWeiboUpdateResourcesUsesOriginalQualityFirst(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"media_items": []map[string]any{{
			"url":      "https://wx2.sinaimg.cn/orj360/cover.jpg",
			"fallback": "assets/cover.svg",
		}},
	}
	resources := prepareWeiboUpdateResources(data)
	if len(resources) != 1 {
		t.Fatalf("resources = %#v", resources)
	}
	resource := resources[0]
	if resource.ID != "weibo-media-0" || resource.URL != "https://wx2.sinaimg.cn/large/cover.jpg" || resource.Referer != "https://weibo.com/" {
		t.Fatalf("resource = %#v", resource)
	}
	wantFallbacks := []string{
		"https://wx2.sinaimg.cn/mw2000/cover.jpg",
		"https://wx2.sinaimg.cn/mw1024/cover.jpg",
		"https://wx2.sinaimg.cn/mw690/cover.jpg",
		"https://wx2.sinaimg.cn/bmiddle/cover.jpg",
	}
	if len(resource.FallbackURLs) != len(wantFallbacks) {
		t.Fatalf("fallback URLs = %#v", resource.FallbackURLs)
	}
	for index, want := range wantFallbacks {
		if resource.FallbackURLs[index] != want {
			t.Fatalf("fallback URL %d = %q, want %q", index, resource.FallbackURLs[index], want)
		}
	}
	item := mapSliceValue(data["media_items"])[0]
	if stringScalar(item["resource_id"]) != "weibo-media-0" || stringScalar(item["url"]) != "assets/cover.svg" {
		t.Fatalf("media item = %#v", item)
	}
}

func TestPrepareWeiboUpdateResourcesCoversMainAndRepostMedia(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"media_items": []map[string]any{
			{"url": "https://wx1.sinaimg.cn/large/main-1.jpg"},
			{"url": "assets/grid.svg"},
		},
		"original": map[string]any{
			"media_items": []map[string]any{{"url": "https://wx2.sinaimg.cn/large/repost-1.jpg"}},
		},
	}
	resources := prepareWeiboUpdateResources(data)
	if len(resources) != 2 || resources[0].ID != "weibo-media-0" || resources[1].ID != "weibo-media-1" {
		t.Fatalf("resources = %#v", resources)
	}
	mainItems := mapSliceValue(data["media_items"])
	if stringScalar(mainItems[0]["resource_id"]) != "weibo-media-0" || stringScalar(mainItems[1]["resource_id"]) != "" {
		t.Fatalf("main media items = %#v", mainItems)
	}
	originalItems := mapSliceValue(mapValue(data["original"])["media_items"])
	if stringScalar(originalItems[0]["resource_id"]) != "weibo-media-1" {
		t.Fatalf("repost media items = %#v", originalItems)
	}
}

func TestPrepareWeiboUpdateResourcesDoesNotExposeUnsupportedRemoteURL(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"media_items": []map[string]any{{
			"url":      "https://n.sinaimg.cn/private.jpg",
			"fallback": "assets/grid.svg",
		}},
	}
	if resources := prepareWeiboUpdateResources(data); len(resources) != 0 {
		t.Fatalf("resources = %#v", resources)
	}
	item := mapSliceValue(data["media_items"])[0]
	if stringScalar(item["url"]) != "assets/grid.svg" || stringScalar(item["resource_id"]) != "" {
		t.Fatalf("media item = %#v", item)
	}
}
