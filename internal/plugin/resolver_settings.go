package plugin

import "strings"

func NormalizeResolverSettings(settings ResolverSettings) ResolverSettings {
	settings.Targets = normalizeResolverTargets(settings.Targets)
	settings.Cooldowns.SameLinkSeconds = boundedSetting(settings.Cooldowns.SameLinkSeconds, 10, 1, 3600)
	settings.Cooldowns.SamePlatformSeconds = boundedSetting(settings.Cooldowns.SamePlatformSeconds, 10, 1, 3600)
	media := &settings.Media
	media.LiveRecordSeconds = boundedSetting(media.LiveRecordSeconds, 30, 5, 50)
	media.VideoSizeLimitMB = boundedSetting(media.VideoSizeLimitMB, 70, 1, 2048)
	media.ImageForwardThreshold = boundedSettingAllowZero(media.ImageForwardThreshold, 0, 100)
	media.ImageBatchSize = boundedSetting(media.ImageBatchSize, 50, 1, 100)
	media.MediaConcurrency = boundedSetting(media.MediaConcurrency, 1, 1, 8)
	switch strings.ToLower(strings.TrimSpace(media.VideoCodec)) {
	case "av1", "hevc", "avc":
		media.VideoCodec = strings.ToLower(strings.TrimSpace(media.VideoCodec))
	default:
		media.VideoCodec = "auto"
	}
	media.BilibiliMaxDurationSeconds = boundedSetting(media.BilibiliMaxDurationSeconds, 480, 1, 7200)
	media.BilibiliResolution = normalizeResolution(media.BilibiliResolution, 480)
	media.BilibiliFileSizeLimitMB = boundedSetting(media.BilibiliFileSizeLimitMB, 100, 1, 2048)
	media.BilibiliMinResolution = normalizeResolution(media.BilibiliMinResolution, 360)
	if media.BilibiliMinResolution > media.BilibiliResolution {
		media.BilibiliMinResolution = media.BilibiliResolution
	}
	media.BilibiliBangumiResolution = normalizeResolution(media.BilibiliBangumiResolution, 480)
	media.BilibiliBangumiMaxSeconds = boundedSetting(media.BilibiliBangumiMaxSeconds, 1800, 1, 10800)
	media.DouyinMaxDurationSeconds = boundedSetting(media.DouyinMaxDurationSeconds, 480, 1, 7200)
	if media.DouyinResolution != 720 && media.DouyinResolution != 1080 {
		media.DouyinResolution = 1080
	}
	return settings
}

func normalizeResolverTargets(items []ResolverTarget) []ResolverTarget {
	seen := map[string]bool{}
	result := make([]ResolverTarget, 0, len(items))
	for _, item := range items {
		item.TargetType = NormalizedTargetType(item.TargetType)
		item.TargetID = strings.TrimSpace(item.TargetID)
		item.TargetName = strings.TrimSpace(item.TargetName)
		if item.TargetID == "" {
			continue
		}
		key := item.TargetType + ":" + item.TargetID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}

func (settings ResolverSettings) Target(targetType, targetID string) (ResolverTarget, bool) {
	targetType = NormalizedTargetType(targetType)
	targetID = strings.TrimSpace(targetID)
	for _, target := range settings.Targets {
		if target.TargetType == targetType && target.TargetID == targetID {
			return target, true
		}
	}
	return ResolverTarget{TargetType: targetType, TargetID: targetID}, false
}

func (target ResolverTarget) PlatformEnabled(platform string) bool {
	switch platform {
	case "bilibili":
		return target.Bilibili
	case "weibo":
		return target.Weibo
	case "douyin":
		return target.Douyin
	default:
		return false
	}
}

func (target *ResolverTarget) SetPlatform(platform string, enabled bool) bool {
	if target == nil {
		return false
	}
	switch platform {
	case "bilibili":
		target.Bilibili = enabled
	case "weibo":
		target.Weibo = enabled
	case "douyin":
		target.Douyin = enabled
	default:
		return false
	}
	return true
}

func boundedSetting(value, fallback, minimum, maximum int) int {
	if value < minimum {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func boundedSettingAllowZero(value, fallback, maximum int) int {
	if value < 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizeResolution(value, fallback int) int {
	switch value {
	case 360, 480, 720, 1080, 2160:
		return value
	default:
		return fallback
	}
}
