package douyin

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/RayleaBot/subscription-hub/internal/plugin"
)

func TestCleanDouyinShareURLStripsTrackingParams(t *testing.T) {
	cleaned, ok := cleanDouyinShareURL("https://www.douyin.com/video/736300123?share_uid=MS4wLjABAAAAxx&tt_from=copy&utm_source=copy&utm_campaign=client_share&utm_medium=android")
	if !ok || cleaned != "https://www.douyin.com/video/736300123" {
		t.Fatalf("cleanDouyinShareURL() = %q, %v", cleaned, ok)
	}
	if cleaned, ok := cleanDouyinShareURL("https://v.douyin.com/AbC123x/"); !ok || cleaned != "https://v.douyin.com/AbC123x/" {
		t.Fatalf("short share URL changed: %q, %v", cleaned, ok)
	}
	if cleaned, ok := cleanDouyinShareURL("https://www.iesdouyin.com/share/video/999/?region=CN&mid=1&u_code=7"); !ok || cleaned != "https://www.iesdouyin.com/share/video/999/" {
		t.Fatalf("iesdouyin share URL = %q, %v", cleaned, ok)
	}
	if cleaned, ok := cleanDouyinShareURL("https://www.douyin.com/note/123?previous_page=app_code_link"); !ok || cleaned != "https://www.douyin.com/note/123" {
		t.Fatalf("note URL = %q, %v", cleaned, ok)
	}
	if _, ok := cleanDouyinShareURL("https://example.com/video/1?a=1"); ok {
		t.Fatal("non-douyin host should not be accepted")
	}
	if _, ok := cleanDouyinShareURL("not a url"); ok {
		t.Fatal("malformed share URL should not be accepted")
	}
}

func TestDouyinAwemeURLPrefersCleanShareOrCanonical(t *testing.T) {
	share := map[string]any{"share_url": "https://www.douyin.com/video/736300123?share_uid=MS4wLjABAAAAxx&tt_from=copy"}
	if got := douyinAwemeURL(share, "736300123", "video"); got != "https://www.douyin.com/video/736300123" {
		t.Fatalf("douyinAwemeURL() = %q", got)
	}
	short := map[string]any{"share_url": "https://v.douyin.com/AbC123x/"}
	if got := douyinAwemeURL(short, "736300123", "video"); got != "https://v.douyin.com/AbC123x/" {
		t.Fatalf("short share URL not preserved: %q", got)
	}
	if got := douyinAwemeURL(map[string]any{}, "736300123", "video"); got != "https://www.douyin.com/video/736300123" {
		t.Fatalf("video canonical URL = %q", got)
	}
	if got := douyinAwemeURL(map[string]any{}, "736300124", "image_text"); got != "https://www.douyin.com/note/736300124" {
		t.Fatalf("note canonical URL = %q", got)
	}
}

func TestDouyinImageMirrorsCollectsURLList(t *testing.T) {
	mirrors := douyinImageMirrors(map[string]any{"url_list": []any{
		"https://p3-pc.douyinpic.com/a.jpeg",
		"https://p6-pc.douyinpic.com/a.jpeg",
		"https://p9-pc.douyinpic.com/a.jpeg",
		"https://p26-pc.douyinpic.com/a.jpeg",
		"https://p11-pc.douyinpic.com/a.jpeg",
	}})
	if len(mirrors) != 4 || mirrors[0] != "https://p3-pc.douyinpic.com/a.jpeg" {
		t.Fatalf("douyinImageMirrors() = %#v", mirrors)
	}
	if got := douyinImageMirrors("https://p3-pc.douyinpic.com/single.jpeg"); len(got) != 1 {
		t.Fatalf("string mirror = %#v", got)
	}
	if got := douyinImageMirrors(map[string]any{"url": "https://p3-pc.douyinpic.com/obj.jpeg"}); len(got) != 1 || got[0] != "https://p3-pc.douyinpic.com/obj.jpeg" {
		t.Fatalf("map url fallback = %#v", got)
	}
	if got := douyinImageMirrors(map[string]any{"url_list": []any{"not-a-url"}}); len(got) != 0 {
		t.Fatalf("invalid url_list entry should be dropped: %#v", got)
	}
}

func TestDouyinVideoCoverSelection(t *testing.T) {
	cover := func(name string) map[string]any {
		return map[string]any{"url_list": []any{
			"https://p3-pc.douyinpic.com/" + name + ".jpeg",
			"https://p6-pc.douyinpic.com/" + name + ".jpeg",
		}}
	}
	for _, test := range []struct {
		name  string
		video map[string]any
		want  string
	}{
		{
			name: "published cover before video frame",
			video: map[string]any{
				"cover_original_scale": cover("published"), "cover": cover("resized"),
				"origin_cover": cover("frame"), "dynamic_cover": cover("animated"),
			},
			want: "published",
		},
		{
			name:  "regular cover before video frame",
			video: map[string]any{"cover": cover("resized"), "origin_cover": cover("frame")},
			want:  "resized",
		},
		{
			name:  "empty original scale does not hide regular cover",
			video: map[string]any{"cover_original_scale": map[string]any{"url_list": []any{}}, "cover": cover("resized")},
			want:  "resized",
		},
		{
			name: "invalid static covers fall back to original frame",
			video: map[string]any{
				"cover_original_scale": map[string]any{"url_list": []any{"invalid", "http://example.com/cover.jpeg"}},
				"cover":                map[string]any{}, "origin_cover": cover("frame"), "dynamic_cover": cover("animated"),
			},
			want: "frame",
		},
		{
			name:  "dynamic cover is last fallback",
			video: map[string]any{"origin_cover": map[string]any{}, "dynamic_cover": cover("animated")},
			want:  "animated",
		},
		{name: "no usable cover", video: map[string]any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			images := douyinAwemeImages(map[string]any{"video": test.video}, "video")
			if test.want == "" {
				if len(images) != 0 {
					t.Fatalf("unexpected images: %#v", images)
				}
				return
			}
			want := douyinImageMirrors(cover(test.want))
			if len(images) != 1 || images[0]["url"] != want[0] || !reflect.DeepEqual(images[0]["candidates"], want) {
				t.Fatalf("cover = %#v, want URLs %v", images, want)
			}
		})
	}
}

func TestDouyinImagePostKeepsImagesBeforeVideoCover(t *testing.T) {
	aweme := map[string]any{
		"images": []any{map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/photo.jpeg"}}},
		"video":  map[string]any{"cover_original_scale": map[string]any{"url_list": []any{"https://p3-pc.douyinpic.com/cover.jpeg"}}},
	}
	images := douyinAwemeImages(aweme, "image_text")
	if len(images) != 1 || images[0]["url"] != "https://p3-pc.douyinpic.com/photo.jpeg" {
		t.Fatalf("image post was replaced by its video cover: %#v", images)
	}
}

func TestDouyinPicRegionMirrorsSwapsRegionHosts(t *testing.T) {
	parsed, _ := url.Parse("https://p3-pc-sign.douyinpic.com/aweme/face.jpeg?x-expires=1780905600&x-signature=abc")
	alts := douyinpicRegionMirrors(parsed)
	if len(alts) != 4 {
		t.Fatalf("douyinpicRegionMirrors() = %#v", alts)
	}
	for _, alt := range alts {
		if !strings.HasSuffix(alt, ".douyinpic.com/aweme/face.jpeg?x-expires=1780905600&x-signature=abc") {
			t.Fatalf("mirror lost path or signature: %q", alt)
		}
	}
	if alts[0] == parsed.String() {
		t.Fatal("mirror should not duplicate the primary host")
	}
	if got := douyinpicRegionMirrors(&url.URL{Scheme: "https", Host: "douyinpic.com", Path: "/a.jpeg"}); got != nil {
		t.Fatalf("regionless host mirrors = %#v", got)
	}
}

func TestPrepareDouyinUpdateResourcesUsesMirrorCandidates(t *testing.T) {
	data := map[string]any{"media_items": []map[string]any{
		{
			"url": "https://p3-pc.douyinpic.com/aweme/cover.jpeg",
			"candidates": []any{
				"https://p3-pc.douyinpic.com/aweme/cover.jpeg",
				"https://p6-pc.douyinpic.com/aweme/cover.jpeg",
				"https://p9-pc.douyinpic.com/aweme/cover.jpeg",
				"https://example.com/untrusted.jpeg",
			},
			"fallback": "assets/cover.svg",
		},
	}}
	resources := prepareDouyinUpdateResources(data)
	if len(resources) != 1 {
		t.Fatalf("resources = %#v", resources)
	}
	if resources[0].URL != "https://p3-pc.douyinpic.com/aweme/cover.jpeg" || !reflect.DeepEqual(resources[0].FallbackURLs, []string{"https://p6-pc.douyinpic.com/aweme/cover.jpeg", "https://p9-pc.douyinpic.com/aweme/cover.jpeg"}) {
		t.Fatalf("resource fallback URLs = %#v", resources[0].FallbackURLs)
	}
}

func TestRenderSubscriptionAuthorDropsRelativeFeedAvatar(t *testing.T) {
	item := plugin.Subscription{UID: "MS4wLjABAAAAone", Name: "测试用户", AvatarURL: "https://p3-pc.douyinpic.com/stored.jpeg"}
	author := plugin.RenderSubscriptionAuthor(item, map[string]any{
		"name": "测试用户", "uid": "MS4wLjABAAAAone", "avatar": "tos-cn-av-0015/relative-uri~c5.jpeg",
	})
	if got := plugin.StringScalar(author["avatar"]); got != "https://p3-pc.douyinpic.com/stored.jpeg" {
		t.Fatalf("relative feed avatar should fall back to stored avatar: %q", got)
	}
	author = plugin.RenderSubscriptionAuthor(item, map[string]any{
		"name": "测试用户", "uid": "MS4wLjABAAAAone", "avatar": "https://p3-pc.douyinpic.com/fresh.jpeg",
	})
	if got := plugin.StringScalar(author["avatar"]); got != "https://p3-pc.douyinpic.com/fresh.jpeg" {
		t.Fatalf("valid feed avatar should win: %q", got)
	}
}
