package plugin

import (
	"encoding/json"
	"testing"
	"time"

	rayleabot "github.com/RayleaBot/RayleaBot/sdk/go"
	"github.com/RayleaBot/plugin-subscription-hub/internal/assets"
)

func TestResolverDefaultConfigIsClosedAndUsesTenSecondLinkCooldown(t *testing.T) {
	var settings Settings
	if err := json.Unmarshal(assets.DefaultConfigJSON, &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Resolver.Targets) != 0 {
		t.Fatalf("resolver targets = %#v, want none", settings.Resolver.Targets)
	}
	if !settings.Resolver.Cooldowns.SameLinkEnabled || settings.Resolver.Cooldowns.SameLinkSeconds != 10 {
		t.Fatalf("same-link cooldown = %#v", settings.Resolver.Cooldowns)
	}
	if settings.Resolver.Cooldowns.SamePlatformEnabled {
		t.Fatal("same-platform cooldown must default off")
	}
	if settings.Resolver.Media.LiveRecordSeconds != 30 || !settings.Resolver.Media.UploadOversize {
		t.Fatalf("media defaults = %#v", settings.Resolver.Media)
	}
	if settings.Resolver.SuperAdminWhitelist {
		t.Fatal("super admin whitelist must default off")
	}
}

func TestResolverCommandsAreRouted(t *testing.T) {
	handler := newWorkflowHandler(t)
	want := map[string]string{
		"解析帮助": "resolver_help", "开启B站解析": "resolver_enable_bilibili", "关闭B站解析": "resolver_disable_bilibili",
		"开启微博解析": "resolver_enable_weibo", "关闭微博解析": "resolver_disable_weibo",
		"开启抖音解析": "resolver_enable_douyin", "关闭抖音解析": "resolver_disable_douyin",
		"开启超管解析": "resolver_enable_super_admin", "关闭超管解析": "resolver_disable_super_admin",
	}
	for command, operation := range want {
		_, got := handler.CommandOperation(command)
		if got != operation {
			t.Fatalf("command %q operation = %q, want %q", command, got, operation)
		}
	}
}

func TestResolverTargetForEventLetsSuperAdminsBypassTargetSwitches(t *testing.T) {
	settings := NormalizeResolverSettings(ResolverSettings{
		SuperAdminWhitelist: true,
		Targets:             []ResolverTarget{{TargetType: "group", TargetID: "100", TargetName: "测试群", Bilibili: true}},
	})
	event := func(actor, targetType, targetID string) *rayleabot.EventContext {
		return &rayleabot.EventContext{SuperAdmins: []string{"7"}, Event: rayleabot.Event{
			Actor:  rayleabot.Actor{ID: actor},
			Target: rayleabot.Target{Type: targetType, ID: targetID},
		}}
	}

	target, enabled := resolverTargetForEvent(settings, event("7", "private", "300"))
	if !enabled || !target.Bilibili || !target.Weibo || !target.Douyin || target.TargetType != "private" || target.TargetID != "300" {
		t.Fatalf("super admin in an unconfigured private chat = %#v, enabled=%t", target, enabled)
	}
	target, enabled = resolverTargetForEvent(settings, event("7", "group", "100"))
	if !enabled || !target.Weibo || target.TargetName != "测试群" {
		t.Fatalf("super admin in a configured group = %#v, enabled=%t", target, enabled)
	}
	if _, enabled := resolverTargetForEvent(settings, event("8", "private", "300")); enabled {
		t.Fatal("whitelist must not open unconfigured chats for ordinary members")
	}
	target, enabled = resolverTargetForEvent(settings, event("8", "group", "100"))
	if !enabled || !target.Bilibili || target.Weibo || target.Douyin {
		t.Fatalf("ordinary member keeps the configured switches: %#v, enabled=%t", target, enabled)
	}

	settings.SuperAdminWhitelist = false
	if _, enabled := resolverTargetForEvent(settings, event("7", "private", "300")); enabled {
		t.Fatal("disabled whitelist must not bypass target switches")
	}
	target, enabled = resolverTargetForEvent(settings, event("7", "group", "100"))
	if !enabled || target.Weibo {
		t.Fatalf("disabled whitelist keeps configured switches for super admins: %#v, enabled=%t", target, enabled)
	}
	if _, enabled := resolverTargetForEvent(NormalizeResolverSettings(ResolverSettings{SuperAdminWhitelist: true}), event("7", "group", "")); enabled {
		t.Fatal("events without a target cannot be resolved")
	}
}

func TestNormalizeResolverSettingsKeepsExplicitTargetSwitches(t *testing.T) {
	settings := NormalizeResolverSettings(ResolverSettings{
		Targets: []ResolverTarget{
			{TargetType: "group", TargetID: " 100 ", TargetName: " 测试群 ", Bilibili: true},
			{TargetType: "group", TargetID: "100", Weibo: true},
			{TargetType: "private", TargetID: "200", Douyin: true},
		},
	})
	if len(settings.Targets) != 2 {
		t.Fatalf("target count = %d, want 2", len(settings.Targets))
	}
	group, ok := settings.Target("group", "100")
	if !ok || !group.Bilibili || group.Weibo || group.Douyin || group.TargetName != "测试群" {
		t.Fatalf("normalized group target = %#v", group)
	}
	private, ok := settings.Target("private", "200")
	if !ok || !private.Douyin {
		t.Fatalf("normalized private target = %#v", private)
	}
}

func TestNormalizeResolverSettingsAppliesMediaDefaultsAndBounds(t *testing.T) {
	settings := NormalizeResolverSettings(ResolverSettings{
		Cooldowns: ResolverCooldownSettings{SameLinkSeconds: 0, SamePlatformSeconds: 4000},
		Media: ResolverMediaSettings{
			LiveRecordSeconds: 30, MediaConcurrency: 9, VideoCodec: "VP9",
			BilibiliResolution: 480, BilibiliMinResolution: 720, DouyinResolution: 480,
		},
	})
	if settings.Cooldowns.SameLinkSeconds != 10 || settings.Cooldowns.SamePlatformSeconds != 3600 {
		t.Fatalf("cooldowns = %#v", settings.Cooldowns)
	}
	if settings.Media.LiveRecordSeconds != 30 || settings.Media.MediaConcurrency != 8 || settings.Media.VideoCodec != "auto" {
		t.Fatalf("media defaults = %#v", settings.Media)
	}
	if settings.Media.BilibiliMinResolution != 480 || settings.Media.DouyinResolution != 1080 {
		t.Fatalf("resolution normalization = %#v", settings.Media)
	}
}

func TestResolverCooldownSeparatesLinksPlatformsAndTargets(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	handler := &Handler{now: func() time.Time { return now }, resolverCooldowns: map[string]time.Time{}}
	settings := ResolverCooldownSettings{SameLinkEnabled: true, SameLinkSeconds: 10}
	group := ResolverTarget{TargetType: "group", TargetID: "100"}

	if handler.resolverCooldownHit(group, "bilibili", "https://www.bilibili.com/video/BV1", settings) {
		t.Fatal("first link unexpectedly hit cooldown")
	}
	if !handler.resolverCooldownHit(group, "bilibili", "https://www.bilibili.com/video/BV1", settings) {
		t.Fatal("same link did not hit cooldown")
	}
	if handler.resolverCooldownHit(group, "bilibili", "https://www.bilibili.com/video/BV2", settings) {
		t.Fatal("different link unexpectedly hit link cooldown")
	}
	if handler.resolverCooldownHit(ResolverTarget{TargetType: "private", TargetID: "100"}, "bilibili", "https://www.bilibili.com/video/BV1", settings) {
		t.Fatal("different target unexpectedly shared cooldown")
	}
	now = now.Add(11 * time.Second)
	if handler.resolverCooldownHit(group, "bilibili", "https://www.bilibili.com/video/BV1", settings) {
		t.Fatal("expired link cooldown still active")
	}
}

func TestResolverPlatformCooldownAppliesAcrossLinks(t *testing.T) {
	handler := &Handler{now: func() time.Time { return time.Unix(100, 0) }, resolverCooldowns: map[string]time.Time{}}
	settings := ResolverCooldownSettings{SamePlatformEnabled: true, SamePlatformSeconds: 10}
	target := ResolverTarget{TargetType: "group", TargetID: "100"}
	if handler.resolverCooldownHit(target, "weibo", "https://weibo.com/one", settings) {
		t.Fatal("first platform event unexpectedly hit cooldown")
	}
	if !handler.resolverCooldownHit(target, "weibo", "https://weibo.com/two", settings) {
		t.Fatal("different link did not hit platform cooldown")
	}
	if handler.resolverCooldownHit(target, "douyin", "https://douyin.com/two", settings) {
		t.Fatal("different platform unexpectedly hit cooldown")
	}
}

func TestCanonicalResolverKeyKeepsContentIdentityAndDropsShareTracking(t *testing.T) {
	got := canonicalResolverKey("https://www.bilibili.com/read/mobile?id=66&spm_id_from=333.1007#reply")
	if got != "https://www.bilibili.com/read/mobile?id=66" {
		t.Fatalf("canonical key = %q", got)
	}
	if first := canonicalResolverKey("https://www.bilibili.com/read/mobile?id=66"); first == canonicalResolverKey("https://www.bilibili.com/read/mobile?id=77") {
		t.Fatal("distinct query-addressed content shared one cooldown key")
	}
	if got := firstResolverURL("看看这个：https://v.douyin.com/example/。复制打开"); got != "https://v.douyin.com/example/" {
		t.Fatalf("shared message url = %q", got)
	}
}
