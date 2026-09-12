package netease_music

import (
	"github.com/RayleaBot/plugin-subscription-hub/internal/plugin"
	"testing"
)

func TestAccountAvatarSources(t *testing.T) {
	policy := New().Avatar
	for _, raw := range []string{"https://p1.music.126.net/fixture.jpg", "https://music.163.com/fixture.jpg"} {
		if _, _, err := plugin.ValidateAvatarSourceURL(raw, policy); err != nil {
			t.Fatalf("allowed avatar: %v", err)
		}
	}
	if _, _, err := plugin.ValidateAvatarSourceURL("https://music.126.net.evil.test/fixture.jpg", policy); err == nil {
		t.Fatal("unrelated host was allowed")
	}
}
