package plugin

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Platform describes a statically assembled source. Its session owns only the
// source clients and caches for one command or check.
type Platform struct {
	ID              string
	Name            string
	SubjectLabel    string
	ListTitle       string
	AllListTitle    string
	Commands        map[string]string
	Services        ServiceCatalog
	ParseSubject    func(string) string
	NewSession      func(SourceActions) Session
	PreviewAliases  []string
	PreviewPrefixes []string
	PreviewDefault  string
	PreviewInputs   map[string]string
	Baseline        *BaselinePolicy
	Avatar          AvatarPolicy
	Cleanup         func(Subscription) []KVSelector
	Account         *AccountAdapter
}

// AccountAdapter supplies the platform-specific credential check and QR login
// implementation for plugin-managed accounts. Both hooks are optional.
type AccountAdapter struct {
	// Validate checks one cookie and classifies the outcome. It must not
	// mutate stored account state.
	Validate func(context.Context, SourceActions, string) AccountValidation
	// NewQRProvider builds one QR login provider per login session.
	NewQRProvider QRLoginProviderFactory
}

type Session interface {
	Resolve(context.Context, ResolveRequest) (Resolution, error)
}

type SearchSession interface {
	Search(context.Context, string) ([]User, error)
	SearchCard(context.Context, string, []User, []string) CardRequest
}

type UserCardSession interface {
	UserCard(context.Context, string, Subscription, *User, []string) CardRequest
}

type CheckSession interface {
	Poll(context.Context, context.Context, []Subscription, time.Time) PollResult
	Prepare(context.Context, Update) (Update, error)
	UpdateCard(Subscription, Update) CardRequest
}

type UpdateCardPreparingSession interface {
	PrepareUpdateCard(context.Context, CardRequest) CardRequest
}

type PreviewSession interface {
	Preview(context.Context, string) (Update, bool, error)
	Sample(string, time.Time) Update
	UpdateCard(Subscription, Update) CardRequest
}

// Update keeps the existing normalized content and template data fields.
type Update = map[string]any

type ResolveRequest struct {
	Query         string
	Purpose       string
	Subscriptions []Subscription
}

type User struct {
	UID       string
	UniqueID  string
	Name      string
	AvatarURL string
	Profile   any
}

type Resolution struct {
	Matched    *User
	Candidates []User
	Message    string
}

func (result Resolution) ManagementResult(platform, query string) map[string]any {
	candidates := make([]any, 0, len(result.Candidates))
	for _, user := range result.Candidates {
		candidates = append(candidates, user.Profile)
	}
	value := map[string]any{"platform": platform, "query": query, "exact": result.Matched != nil, "candidates": candidates}
	if result.Matched != nil {
		value["user"] = result.Matched.Profile
	}
	if result.Message != "" {
		value["message"] = DiagnosticExcerpt(result.Message, 500)
	}
	return value
}

type PollResult struct {
	PauseReasons []string
	FailureKinds []string
	Checked      int
	Updates      []Update
	Errors       []string
	Summary      map[string]any
	ReadyUIDs    map[string]bool
}

type CardRequest struct {
	Template      string
	Data          map[string]any
	Resources     []RenderResource
	Fallback      string
	InlineAvatars bool
}

type BaselinePolicy struct {
	KeyPrefix      string
	Timestamp      bool
	ExemptServices []string
}

type KVSelector struct {
	Prefix string
	Suffix string
}

// AvatarPolicy supplies a source's CDN allowlist and compact-image candidates.
// The common downloader validates the URL and enforces response size limits.
type AvatarPolicy struct {
	Validate   func(*url.URL) (referer string, allowed bool)
	Candidates func(*url.URL) []string
}

type Options struct {
	// MediaTempRoot isolates owned media leases; empty uses the host's plugin cache.
	MediaTempRoot string
	Platforms     []Platform
	Actions       RuntimeActions
	Now           func() time.Time
	Jitter        func() time.Duration
}

type commandRoute struct{ platform, operation string }

type Handler struct {
	platforms           []Platform
	byID                map[string]Platform
	commands            map[string]commandRoute
	actions             RuntimeActions
	now                 func() time.Time
	jitter              func() time.Duration
	schedulerRegistered atomic.Bool
	checkGate           chan struct{}
	schedulerGate       chan struct{}
	checkLogMu          sync.Mutex
	checkLogs           map[string]checkLogState
	resolverMu          sync.Mutex
	resolverCooldowns   map[string]time.Time
	mediaGate           resolverMediaGate
	deferredMedia       *deferredMediaQueue
	accountQR           *QRLoginManager
	accountCheckMu      sync.Mutex
}

var platformIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func NewHandler(options Options) (*Handler, error) {
	if len(options.Platforms) == 0 {
		return nil, errors.New("subscription hub requires at least one platform")
	}
	mediaRoot := options.MediaTempRoot
	if mediaRoot == "" {
		cacheRoot := os.Getenv("RAYLEABOT_PLUGIN_CACHE_DIR")
		if !filepath.IsAbs(cacheRoot) {
			return nil, errors.New("宿主未提供有效的插件缓存目录 RAYLEABOT_PLUGIN_CACHE_DIR")
		}
		mediaRoot = filepath.Join(cacheRoot, "media")
	}
	handler := &Handler{
		byID: map[string]Platform{}, commands: map[string]commandRoute{}, resolverCooldowns: map[string]time.Time{},
		actions: options.Actions, now: options.Now, jitter: options.Jitter,
		deferredMedia: newDeferredMediaQueue(mediaRoot),
		checkGate:     make(chan struct{}, 1), schedulerGate: make(chan struct{}, 1),
	}
	if handler.now == nil {
		handler.now = time.Now
	}
	if handler.jitter == nil {
		handler.jitter = schedulerJitterDelay
	}
	for command, operation := range map[string]string{
		"订阅状态": "status", "订阅列表": "list", "全部订阅列表": "list_all",
		"立即检查订阅": "check", "预览订阅卡片": "preview",
		"解析帮助":   "resolver_help",
		"开启B站解析": "resolver_enable_bilibili", "关闭B站解析": "resolver_disable_bilibili",
		"开启微博解析": "resolver_enable_weibo", "关闭微博解析": "resolver_disable_weibo",
		"开启抖音解析": "resolver_enable_douyin", "关闭抖音解析": "resolver_disable_douyin",
		"开启超管解析": "resolver_enable_super_admin", "关闭超管解析": "resolver_disable_super_admin",
	} {
		handler.commands[command] = commandRoute{operation: operation}
	}
	aliases := map[string]bool{}
	for _, platform := range options.Platforms {
		if !platformIDPattern.MatchString(platform.ID) || strings.TrimSpace(platform.Name) == "" ||
			platform.NewSession == nil || platform.ParseSubject == nil || len(platform.Services.order) == 0 {
			return nil, fmt.Errorf("invalid platform declaration %q", platform.ID)
		}
		if _, exists := handler.byID[platform.ID]; exists {
			return nil, fmt.Errorf("duplicate platform %q", platform.ID)
		}
		if err := platform.Services.validate(); err != nil {
			return nil, fmt.Errorf("platform %q: %w", platform.ID, err)
		}
		if platform.PreviewDefault != "" && platform.Services.Normalize(platform.PreviewDefault) != platform.PreviewDefault {
			return nil, fmt.Errorf("invalid default preview service for platform %q", platform.ID)
		}
		if platform.Baseline != nil {
			if strings.TrimSpace(platform.Baseline.KeyPrefix) == "" {
				return nil, fmt.Errorf("platform %q has no baseline key", platform.ID)
			}
			baseline := *platform.Baseline
			baseline.ExemptServices = slices.Clone(baseline.ExemptServices)
			platform.Baseline = &baseline
		}
		platform.Commands = maps.Clone(platform.Commands)
		platform.Services = platform.Services.clone()
		platform.PreviewAliases = slices.Clone(platform.PreviewAliases)
		platform.PreviewPrefixes = slices.Clone(platform.PreviewPrefixes)
		platform.PreviewInputs = maps.Clone(platform.PreviewInputs)
		for index, alias := range platform.PreviewAliases {
			alias = strings.ToLower(strings.TrimSpace(alias))
			if alias == "" || aliases[alias] {
				return nil, fmt.Errorf("duplicate or empty preview alias %q", alias)
			}
			aliases[alias] = true
			platform.PreviewAliases[index] = alias
		}
		for index, prefix := range platform.PreviewPrefixes {
			prefix = strings.ToLower(strings.TrimSpace(prefix))
			if !slices.Contains(platform.PreviewAliases, prefix) {
				return nil, fmt.Errorf("invalid preview prefix for platform %q", platform.ID)
			}
			platform.PreviewPrefixes[index] = prefix
		}
		for command, operation := range platform.Commands {
			if strings.TrimSpace(command) == "" || !slices.Contains([]string{"add", "remove", "search", "list", "list_all"}, operation) {
				return nil, fmt.Errorf("invalid command for platform %q", platform.ID)
			}
			if _, exists := handler.commands[command]; exists {
				return nil, fmt.Errorf("duplicate command %q", command)
			}
			handler.commands[command] = commandRoute{platform: platform.ID, operation: operation}
		}
		handler.platforms = append(handler.platforms, platform)
		handler.byID[platform.ID] = platform
	}
	if err := addCommandAliases(handler.commands); err != nil {
		return nil, err
	}
	handler.initAccountQRManager()
	return handler, nil
}

func (handler *Handler) CommandOperation(command string) (string, string) {
	route := handler.commands[strings.TrimSpace(command)]
	return route.platform, route.operation
}

func (handler *Handler) platformName(id string) string { return handler.byID[id].Name }

func (handler *Handler) normalizePlatform(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if _, ok := handler.byID[id]; ok {
		return id
	}
	return ""
}
