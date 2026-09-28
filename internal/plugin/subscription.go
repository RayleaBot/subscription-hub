package plugin

type Settings struct {
	Enabled                     bool             `json:"enabled"`
	DeliveryMaxAgeMinutes       int              `json:"delivery_max_age_minutes"`
	AccountCheckIntervalMinutes int              `json:"account_check_interval_minutes"`
	AccountBrowserMode          string           `json:"account_browser_mode"`
	AccountBrowserRemoteURL     string           `json:"account_browser_remote_debugging_url"`
	Subscriptions               []Subscription   `json:"subscriptions"`
	Resolver                    ResolverSettings `json:"resolver"`
}

type ResolverSettings struct {
	Targets             []ResolverTarget         `json:"targets"`
	SuperAdminWhitelist bool                     `json:"super_admin_whitelist"`
	Cooldowns           ResolverCooldownSettings `json:"cooldowns"`
	Media               ResolverMediaSettings    `json:"media"`
}

type ResolverTarget struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	TargetName string `json:"target_name,omitempty"`
	Bilibili   bool   `json:"bilibili"`
	Weibo      bool   `json:"weibo"`
	Douyin     bool   `json:"douyin"`
}

type ResolverCooldownSettings struct {
	SameLinkEnabled     bool `json:"same_link_enabled"`
	SameLinkSeconds     int  `json:"same_link_seconds"`
	SamePlatformEnabled bool `json:"same_platform_enabled"`
	SamePlatformSeconds int  `json:"same_platform_seconds"`
}

type ResolverMediaSettings struct {
	LiveRecordSeconds          int    `json:"live_record_seconds"`
	VideoSizeLimitMB           int    `json:"video_size_limit_mb"`
	UploadOversize             bool   `json:"upload_oversize"`
	ImageForwardThreshold      int    `json:"image_forward_threshold"`
	ImageBatchSize             int    `json:"image_batch_size"`
	MediaConcurrency           int    `json:"media_concurrency"`
	VideoCodec                 string `json:"video_codec"`
	CompatibilityTranscode     bool   `json:"compatibility_transcode"`
	BilibiliMaxDurationSeconds int    `json:"bilibili_max_duration_seconds"`
	BilibiliResolution         int    `json:"bilibili_resolution"`
	BilibiliSmartResolution    bool   `json:"bilibili_smart_resolution"`
	BilibiliFileSizeLimitMB    int    `json:"bilibili_file_size_limit_mb"`
	BilibiliMinResolution      int    `json:"bilibili_min_resolution"`
	BilibiliBangumiDirect      bool   `json:"bilibili_bangumi_direct"`
	BilibiliBangumiResolution  int    `json:"bilibili_bangumi_resolution"`
	BilibiliBangumiMaxSeconds  int    `json:"bilibili_bangumi_max_seconds"`
	DouyinMaxDurationSeconds   int    `json:"douyin_max_duration_seconds"`
	DouyinResolution           int    `json:"douyin_resolution"`
	DouyinMergeBGM             bool   `json:"douyin_merge_bgm"`
}

type Subscription struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	UID      string `json:"uid"`
	// UniqueID 是可修改的展示标识，UID 是稳定的订阅来源标识。
	UniqueID    string       `json:"unique_id,omitempty"`
	Name        string       `json:"name"`
	AvatarURL   string       `json:"avatar_url,omitempty"`
	TargetType  string       `json:"target_type"`
	TargetID    string       `json:"target_id"`
	TargetName  string       `json:"target_name,omitempty"`
	Services    []string     `json:"services"`
	Subscribers []Subscriber `json:"subscribers"`
	Enabled     bool         `json:"enabled"`
}

type Subscriber struct {
	ID            string `json:"id"`
	Nickname      string `json:"nickname"`
	GroupNickname string `json:"group_nickname,omitempty"`
	Title         string `json:"title,omitempty"`
	BaseRole      string `json:"base_role,omitempty"`
	Role          string `json:"role,omitempty"`
	RoleLabel     string `json:"role_label,omitempty"`
	AvatarURL     string `json:"avatar_url,omitempty"`
}
