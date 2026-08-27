package plugin

type Settings struct {
	Enabled               bool           `json:"enabled"`
	DeliveryMaxAgeMinutes int            `json:"delivery_max_age_minutes"`
	Subscriptions         []Subscription `json:"subscriptions"`
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
