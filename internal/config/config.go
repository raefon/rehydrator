package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed default.yaml
var DefaultConfigYAML string

type JellyfinInventoryConfig struct {
	MarkMissingInArr   bool              `yaml:"mark_missing_in_arr"`
	Enabled            bool              `yaml:"enabled"`
	IntervalSeconds    int               `yaml:"interval_seconds"`
	MaxRefreshesPerRun int               `yaml:"max_refreshes_per_run"`
	CooldownMinutes    int               `yaml:"cooldown_minutes"`
	PathMappings       map[string]string `yaml:"path_mappings"`
}

type JellyfinConfig struct {
	InventorySync          JellyfinInventoryConfig `yaml:"inventory_sync"`
	RefreshMode            string                  `yaml:"refresh_mode"`
	MaxRefreshesPerRun     int                     `yaml:"max_refreshes_per_run"`
	RefreshDedupeMinutes   int                     `yaml:"refresh_dedupe_minutes"`
	Enabled                bool                    `yaml:"enabled"`
	URL                    string                  `yaml:"url"`
	APIKey                 string                  `yaml:"api_key"`
	RefreshAfterRearm      bool                    `yaml:"refresh_after_rearm"`
	RefreshAfterVisibility bool                    `yaml:"refresh_after_visibility"`
	RefreshAfterImport     bool                    `yaml:"refresh_after_import"`

	RefreshDelaySeconds int `yaml:"refresh_delay_seconds"`
	TimeoutSeconds      int `yaml:"timeout_seconds"`
}

type Config struct {
	Role       string
	Gateway    GatewayConfig `yaml:"gateway"`
	FileHealth FileHealthConfig
	Jellyfin   JellyfinConfig

	PostgresURL          string
	Tenant               string
	LibraryEdition       string
	LibraryPaths         []string
	Seerr4KControllerURL string

	RadarrURL    string
	RadarrAPIKey string

	SonarrURL    string
	SonarrAPIKey string

	SeerrURL    string
	SeerrAPIKey string

	SeerrSyncEnabled         bool
	SeerrSyncIntervalSeconds int
	SeerrSyncLimit           int
	SeerrSyncInterval        time.Duration

	CSIPath    string
	HealthAddr string

	APIEnabled      bool
	APIToken        string
	APIRequireToken bool

	MetricsEnabled bool

	PlaybackEnabled              bool
	PlaybackRearmOnPlay          bool
	PlaybackCooldownSeconds      int
	PlaybackCooldown             time.Duration
	PlaybackIgnoredTitles        []string
	PlaybackIgnoredTitleContains []string

	SelfHealEnabled            bool
	SelfHealIntervalSeconds    int
	SelfHealRefreshAvailable   bool
	SelfHealRecentHours        int
	SelfHealMaxRefreshesPerRun int
	SelfHealInterval           time.Duration

	RadarrSyncEnabled         bool
	RadarrSyncIntervalSeconds int
	RadarrSyncInterval        time.Duration

	SonarrSyncEnabled         bool
	SonarrSyncIntervalSeconds int
	SonarrSyncInterval        time.Duration

	RearmEnabled    bool
	MaxRearmsPerRun int

	ReconcileIntervalSeconds    int
	CSIVisibilityTimeoutSeconds int
	CSIVisibilityPollSeconds    int
	CSIVisibilityRetrySeconds   int

	RcloneRCEnabled           bool
	RcloneRCURL               string
	RcloneRCUsername          string
	RcloneRCPassword          string
	RcloneRCRefreshAfterRearm bool
	RcloneRCTimeoutSeconds    int

	CacheGraceHours int

	ReconcileInterval    time.Duration
	CSIVisibilityTimeout time.Duration
	CSIVisibilityPoll    time.Duration
	CSIVisibilityRetry   time.Duration
	RcloneRCTimeout      time.Duration
	CacheGrace           time.Duration

	MaxRetries        int
	ConcurrentWorkers int
	DBAutoMigrate     bool

	ConfigPath    string
	ConfigCreated bool
}

type fileConfig struct {
	Role                 string           `yaml:"role"`
	Gateway              GatewayConfig    `yaml:"gateway"`
	FileHealth           FileHealthConfig `yaml:"file_health"`
	Jellyfin             JellyfinConfig   `yaml:"jellyfin"`
	PostgresURL          string           `yaml:"postgres_url"`
	Tenant               string           `yaml:"tenant"`
	LibraryEdition       string           `yaml:"library_edition"`
	LibraryPaths         []string         `yaml:"library_paths"`
	Seerr4KControllerURL string           `yaml:"seerr_4k_controller_url"`

	Radarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"radarr"`

	Sonarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"sonarr"`

	Seerr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
		Sync   struct {
			Enabled         *bool `yaml:"enabled"`
			IntervalSeconds int   `yaml:"interval_seconds"`
			Limit           int   `yaml:"limit"`
		} `yaml:"sync"`
	} `yaml:"seerr"`

	CSIPath    string `yaml:"csi_path"`
	HealthAddr string `yaml:"health_addr"`

	API struct {
		Enabled      *bool  `yaml:"enabled"`
		Token        string `yaml:"token"`
		RequireToken *bool  `yaml:"require_token"`
	} `yaml:"api"`

	Metrics struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"metrics"`

	Playback struct {
		Enabled              *bool    `yaml:"enabled"`
		RearmOnPlay          *bool    `yaml:"rearm_on_play"`
		CooldownSeconds      int      `yaml:"cooldown_seconds"`
		IgnoredTitles        []string `yaml:"ignored_titles"`
		IgnoredTitleContains []string `yaml:"ignored_title_contains"`
	} `yaml:"playback"`

	SelfHeal struct {
		Enabled            *bool `yaml:"enabled"`
		IntervalSeconds    int   `yaml:"interval_seconds"`
		RefreshAvailable   *bool `yaml:"refresh_available"`
		RecentHours        int   `yaml:"recent_hours"`
		MaxRefreshesPerRun int   `yaml:"max_refreshes_per_run"`
	} `yaml:"self_heal"`

	RadarrSync struct {
		Enabled         *bool `yaml:"enabled"`
		IntervalSeconds int   `yaml:"interval_seconds"`
	} `yaml:"radarr_sync"`

	SonarrSync struct {
		Enabled         *bool `yaml:"enabled"`
		IntervalSeconds int   `yaml:"interval_seconds"`
	} `yaml:"sonarr_sync"`

	RearmEnabled    *bool `yaml:"rearm_enabled"`
	MaxRearmsPerRun int   `yaml:"max_rearms_per_run"`

	ReconcileIntervalSeconds int `yaml:"reconcile_interval_seconds"`
	CSI                      struct {
		VisibilityTimeoutSeconds int `yaml:"visibility_timeout_seconds"`
		VisibilityPollSeconds    int `yaml:"visibility_poll_seconds"`
		VisibilityRetrySeconds   int `yaml:"visibility_retry_seconds"`
	} `yaml:"csi"`
	RcloneRC struct {
		Enabled           *bool  `yaml:"enabled"`
		URL               string `yaml:"url"`
		Username          string `yaml:"username"`
		Password          string `yaml:"password"`
		RefreshAfterRearm *bool  `yaml:"refresh_after_rearm"`
		TimeoutSeconds    int    `yaml:"timeout_seconds"`
	} `yaml:"rclone_rc"`
	CacheGraceHours   int  `yaml:"cache_grace_hours"`
	MaxRetries        int  `yaml:"max_retries"`
	ConcurrentWorkers int  `yaml:"concurrent_workers"`
	DBAutoMigrate     bool `yaml:"db_auto_migrate"`
}

func Load(configPath string) (Config, error) { return LoadWithRole(configPath, "") }

// LoadWithRole applies a command-line role override before role-specific validation.
func LoadWithRole(configPath, role string) (Config, error) {
	cfg := defaults()
	cfg.ConfigPath = configPath

	if configPath != "" {
		created, err := ensureConfigFile(configPath)
		if err != nil {
			return cfg, err
		}
		cfg.ConfigCreated = created

		fc, err := readFileConfig(configPath)
		if err != nil {
			return cfg, err
		}
		applyFileConfig(&cfg, fc)
	}

	if err := rejectRetiredEnvironment(); err != nil {
		return cfg, err
	}
	applyEnvOverrides(&cfg)
	if role != "" {
		cfg.Role = role
	}
	hydrateDurations(&cfg)

	if err := validate(cfg); err != nil {
		if cfg.ConfigCreated {
			return cfg, fmt.Errorf("%w; default config was created at %s, edit it or provide environment overrides", err, cfg.ConfigPath)
		}
		return cfg, err
	}

	return cfg, nil
}

func defaults() Config {
	return Config{
		Role:                         RoleCombined,
		FileHealth:                   DefaultFileHealth(),
		Gateway:                      DefaultGateway(),
		Jellyfin:                     JellyfinConfig{InventorySync: JellyfinInventoryConfig{Enabled: true, IntervalSeconds: 1800, MaxRefreshesPerRun: 5, CooldownMinutes: 360}, MaxRefreshesPerRun: 5, RefreshDedupeMinutes: 10, URL: "http://jellyfin:8096", RefreshAfterRearm: true, RefreshAfterVisibility: true, RefreshAfterImport: true, RefreshDelaySeconds: 45, TimeoutSeconds: 20},
		RadarrURL:                    "http://radarr:7878",
		SonarrURL:                    "http://sonarr:8989",
		CSIPath:                      "/storage/media",
		HealthAddr:                   ":8080",
		APIEnabled:                   true,
		APIRequireToken:              true,
		MetricsEnabled:               true,
		PlaybackEnabled:              true,
		PlaybackRearmOnPlay:          true,
		PlaybackCooldownSeconds:      60,
		PlaybackIgnoredTitles:        nil,
		PlaybackIgnoredTitleContains: nil,
		SelfHealEnabled:              true,
		SelfHealIntervalSeconds:      300,
		SelfHealRefreshAvailable:     true,
		SelfHealRecentHours:          24,
		SelfHealMaxRefreshesPerRun:   5,
		RadarrSyncEnabled:            true,
		RadarrSyncIntervalSeconds:    60,
		SonarrSyncEnabled:            false,
		SonarrSyncIntervalSeconds:    60,
		SeerrURL:                     "http://seerr:5055",
		SeerrSyncEnabled:             false,
		SeerrSyncIntervalSeconds:     120,
		SeerrSyncLimit:               100,
		RearmEnabled:                 true,
		MaxRearmsPerRun:              3,
		ReconcileIntervalSeconds:     30,
		CSIVisibilityTimeoutSeconds:  900,
		CSIVisibilityPollSeconds:     10,
		CSIVisibilityRetrySeconds:    60,
		RcloneRCURL:                  "http://localhost:5572",
		RcloneRCTimeoutSeconds:       10,
		CacheGraceHours:              24,
		MaxRetries:                   10,
		ConcurrentWorkers:            2,
		DBAutoMigrate:                false,
	}
}

func ensureConfigFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return false, err
		}
	}

	if err := os.WriteFile(path, []byte(DefaultConfigYAML), 0o600); err != nil {
		return false, err
	}

	return true, nil
}

func readFileConfig(path string) (fileConfig, error) {
	fc := fileConfig{Gateway: DefaultGateway(), Jellyfin: defaults().Jellyfin, FileHealth: DefaultFileHealth()}

	data, err := os.ReadFile(path)
	if err != nil {
		return fc, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fc); err != nil {
		return fc, fmt.Errorf("invalid or removed configuration; see docs/configuration.md: %w", err)
	}

	return fc, nil
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Role != "" {
		cfg.Role = fc.Role
	}
	cfg.Gateway = fc.Gateway
	cfg.FileHealth = fc.FileHealth

	cfg.Jellyfin = fc.Jellyfin
	if fc.PostgresURL != "" {
		cfg.PostgresURL = fc.PostgresURL
	}
	cfg.LibraryEdition = fc.LibraryEdition
	cfg.LibraryPaths = fc.LibraryPaths
	cfg.Seerr4KControllerURL = fc.Seerr4KControllerURL
	if fc.Tenant != "" {
		cfg.Tenant = fc.Tenant
	}

	if fc.Radarr.URL != "" {
		cfg.RadarrURL = fc.Radarr.URL
	}
	if fc.Radarr.APIKey != "" {
		cfg.RadarrAPIKey = fc.Radarr.APIKey
	}

	if fc.Sonarr.URL != "" {
		cfg.SonarrURL = fc.Sonarr.URL
	}
	if fc.Sonarr.APIKey != "" {
		cfg.SonarrAPIKey = fc.Sonarr.APIKey
	}

	if fc.Seerr.URL != "" {
		cfg.SeerrURL = fc.Seerr.URL
	}
	if fc.Seerr.APIKey != "" {
		cfg.SeerrAPIKey = fc.Seerr.APIKey
	}
	if fc.Seerr.Sync.Enabled != nil {
		cfg.SeerrSyncEnabled = *fc.Seerr.Sync.Enabled
	}
	if fc.Seerr.Sync.IntervalSeconds > 0 {
		cfg.SeerrSyncIntervalSeconds = fc.Seerr.Sync.IntervalSeconds
	}
	if fc.Seerr.Sync.Limit > 0 {
		cfg.SeerrSyncLimit = fc.Seerr.Sync.Limit
	}

	if fc.CSIPath != "" {
		cfg.CSIPath = fc.CSIPath
	}
	if fc.HealthAddr != "" {
		cfg.HealthAddr = fc.HealthAddr
	}
	if fc.API.Enabled != nil {
		cfg.APIEnabled = *fc.API.Enabled
	}
	if fc.API.Token != "" {
		cfg.APIToken = fc.API.Token
	}
	if fc.API.RequireToken != nil {
		cfg.APIRequireToken = *fc.API.RequireToken
	}
	if fc.Metrics.Enabled != nil {
		cfg.MetricsEnabled = *fc.Metrics.Enabled
	}
	if fc.Playback.Enabled != nil {
		cfg.PlaybackEnabled = *fc.Playback.Enabled
	}
	if fc.Playback.RearmOnPlay != nil {
		cfg.PlaybackRearmOnPlay = *fc.Playback.RearmOnPlay
	}
	if fc.Playback.CooldownSeconds > 0 {
		cfg.PlaybackCooldownSeconds = fc.Playback.CooldownSeconds
	}
	if len(fc.Playback.IgnoredTitles) > 0 {
		cfg.PlaybackIgnoredTitles = normalizeStringList(fc.Playback.IgnoredTitles)
	}
	if len(fc.Playback.IgnoredTitleContains) > 0 {
		cfg.PlaybackIgnoredTitleContains = normalizeStringList(fc.Playback.IgnoredTitleContains)
	}
	if fc.SelfHeal.Enabled != nil {
		cfg.SelfHealEnabled = *fc.SelfHeal.Enabled
	}
	if fc.SelfHeal.IntervalSeconds > 0 {
		cfg.SelfHealIntervalSeconds = fc.SelfHeal.IntervalSeconds
	}
	if fc.SelfHeal.RefreshAvailable != nil {
		cfg.SelfHealRefreshAvailable = *fc.SelfHeal.RefreshAvailable
	}
	if fc.SelfHeal.RecentHours > 0 {
		cfg.SelfHealRecentHours = fc.SelfHeal.RecentHours
	}
	if fc.SelfHeal.MaxRefreshesPerRun > 0 {
		cfg.SelfHealMaxRefreshesPerRun = fc.SelfHeal.MaxRefreshesPerRun
	}
	if fc.RadarrSync.Enabled != nil {
		cfg.RadarrSyncEnabled = *fc.RadarrSync.Enabled
	}
	if fc.RadarrSync.IntervalSeconds > 0 {
		cfg.RadarrSyncIntervalSeconds = fc.RadarrSync.IntervalSeconds
	}
	if fc.SonarrSync.Enabled != nil {
		cfg.SonarrSyncEnabled = *fc.SonarrSync.Enabled
	}
	if fc.SonarrSync.IntervalSeconds > 0 {
		cfg.SonarrSyncIntervalSeconds = fc.SonarrSync.IntervalSeconds
	}

	if fc.RearmEnabled != nil {
		cfg.RearmEnabled = *fc.RearmEnabled
	}

	if fc.MaxRearmsPerRun > 0 {
		cfg.MaxRearmsPerRun = fc.MaxRearmsPerRun
	}
	if fc.ReconcileIntervalSeconds > 0 {
		cfg.ReconcileIntervalSeconds = fc.ReconcileIntervalSeconds
	}

	if fc.CSI.VisibilityTimeoutSeconds > 0 {
		cfg.CSIVisibilityTimeoutSeconds = fc.CSI.VisibilityTimeoutSeconds
	}
	if fc.CSI.VisibilityPollSeconds > 0 {
		cfg.CSIVisibilityPollSeconds = fc.CSI.VisibilityPollSeconds
	}
	if fc.CSI.VisibilityRetrySeconds > 0 {
		cfg.CSIVisibilityRetrySeconds = fc.CSI.VisibilityRetrySeconds
	}
	if fc.RcloneRC.Enabled != nil {
		cfg.RcloneRCEnabled = *fc.RcloneRC.Enabled
	}
	if fc.RcloneRC.URL != "" {
		cfg.RcloneRCURL = fc.RcloneRC.URL
	}
	if fc.RcloneRC.Username != "" {
		cfg.RcloneRCUsername = fc.RcloneRC.Username
	}
	if fc.RcloneRC.Password != "" {
		cfg.RcloneRCPassword = fc.RcloneRC.Password
	}
	if fc.RcloneRC.RefreshAfterRearm != nil {
		cfg.RcloneRCRefreshAfterRearm = *fc.RcloneRC.RefreshAfterRearm
	}
	if fc.RcloneRC.TimeoutSeconds > 0 {
		cfg.RcloneRCTimeoutSeconds = fc.RcloneRC.TimeoutSeconds
	}
	if fc.CacheGraceHours > 0 {
		cfg.CacheGraceHours = fc.CacheGraceHours
	}
	if fc.MaxRetries > 0 {
		cfg.MaxRetries = fc.MaxRetries
	}
	if fc.ConcurrentWorkers > 0 {
		cfg.ConcurrentWorkers = fc.ConcurrentWorkers
	}

	cfg.DBAutoMigrate = fc.DBAutoMigrate
}

func applyEnvOverrides(cfg *Config) {
	cfg.Role = getenv("REHYDRATOR_ROLE", cfg.Role)
	cfg.Gateway.RadarrCategory = getenv("GATEWAY_RADARR_CATEGORY", cfg.Gateway.RadarrCategory)
	cfg.Gateway.SonarrCategory = getenv("GATEWAY_SONARR_CATEGORY", cfg.Gateway.SonarrCategory)
	cfg.Gateway.OIDC.Enabled = getenvBool("GATEWAY_OIDC_ENABLED", cfg.Gateway.OIDC.Enabled)
	cfg.Gateway.OIDC.IssuerURL = getenv("GATEWAY_OIDC_ISSUER_URL", cfg.Gateway.OIDC.IssuerURL)
	cfg.Gateway.OIDC.ClientID = getenv("GATEWAY_OIDC_CLIENT_ID", cfg.Gateway.OIDC.ClientID)
	cfg.Gateway.OIDC.ClientSecret = getenv("GATEWAY_OIDC_CLIENT_SECRET", cfg.Gateway.OIDC.ClientSecret)
	cfg.Gateway.OIDC.RedirectURL = getenv("GATEWAY_OIDC_REDIRECT_URL", cfg.Gateway.OIDC.RedirectURL)
	cfg.Gateway.DashboardExternalAuth = getenvBool("GATEWAY_DASHBOARD_EXTERNAL_AUTH", cfg.Gateway.DashboardExternalAuth)
	cfg.Gateway.Enabled = getenvBool("GATEWAY_ENABLED", cfg.Gateway.Enabled)
	cfg.Gateway.RecoverMissing = getenvBool("GATEWAY_RECOVER_MISSING", cfg.Gateway.RecoverMissing)
	cfg.Gateway.APIKey = getenv("GATEWAY_API_KEY", cfg.Gateway.APIKey)
	cfg.Gateway.QBUsername = getenv("GATEWAY_QB_USERNAME", cfg.Gateway.QBUsername)
	cfg.Gateway.QBPassword = getenv("GATEWAY_QB_PASSWORD", cfg.Gateway.QBPassword)
	cfg.Gateway.SABAPIKey = getenv("GATEWAY_SAB_API_KEY", cfg.Gateway.SABAPIKey)
	cfg.Gateway.DAVUsername = getenv("GATEWAY_DAV_USERNAME", cfg.Gateway.DAVUsername)
	cfg.Gateway.DAVPassword = getenv("GATEWAY_DAV_PASSWORD", cfg.Gateway.DAVPassword)
	cfg.Gateway.MountPath = getenv("GATEWAY_MOUNT_PATH", cfg.Gateway.MountPath)
	cfg.Gateway.DownloadPath = getenv("GATEWAY_DOWNLOAD_PATH", cfg.Gateway.DownloadPath)
	cfg.Gateway.ActivePollIntervalSeconds = getenvInt("GATEWAY_ACTIVE_POLL_INTERVAL_SECONDS", cfg.Gateway.ActivePollIntervalSeconds)
	cfg.Gateway.SubmitIntervalSeconds = getenvInt("GATEWAY_SUBMIT_INTERVAL_SECONDS", cfg.Gateway.SubmitIntervalSeconds)
	cfg.FileHealth.CanaryPath = getenv("FILE_HEALTH_CANARY_PATH", cfg.FileHealth.CanaryPath)
	cfg.Jellyfin.MaxRefreshesPerRun = getenvInt("JELLYFIN_MAX_REFRESHES_PER_RUN", cfg.Jellyfin.MaxRefreshesPerRun)
	cfg.Jellyfin.RefreshDedupeMinutes = getenvInt("JELLYFIN_REFRESH_DEDUPE_MINUTES", cfg.Jellyfin.RefreshDedupeMinutes)
	cfg.Jellyfin.RefreshMode = getenv("JELLYFIN_REFRESH_MODE", cfg.Jellyfin.RefreshMode)
	cfg.Jellyfin.Enabled = getenvBool("JELLYFIN_ENABLED", cfg.Jellyfin.Enabled)
	cfg.Jellyfin.URL = getenv("JELLYFIN_URL", cfg.Jellyfin.URL)
	cfg.Jellyfin.APIKey = getenv("JELLYFIN_API_KEY", cfg.Jellyfin.APIKey)
	cfg.Jellyfin.RefreshAfterRearm = getenvBool("JELLYFIN_REFRESH_AFTER_REARM", cfg.Jellyfin.RefreshAfterRearm)
	cfg.Jellyfin.RefreshAfterVisibility = getenvBool("JELLYFIN_REFRESH_AFTER_VISIBILITY", cfg.Jellyfin.RefreshAfterVisibility)
	cfg.Jellyfin.RefreshAfterImport = getenvBool("JELLYFIN_REFRESH_AFTER_IMPORT", cfg.Jellyfin.RefreshAfterImport)
	cfg.Jellyfin.RefreshDelaySeconds = getenvInt("JELLYFIN_REFRESH_DELAY_SECONDS", cfg.Jellyfin.RefreshDelaySeconds)
	cfg.Jellyfin.InventorySync.MarkMissingInArr = getenvBool("JELLYFIN_INVENTORY_SYNC_MARK_MISSING_IN_ARR", cfg.Jellyfin.InventorySync.MarkMissingInArr)
	cfg.Jellyfin.InventorySync.Enabled = getenvBool("JELLYFIN_INVENTORY_SYNC_ENABLED", cfg.Jellyfin.InventorySync.Enabled)
	cfg.Jellyfin.InventorySync.IntervalSeconds = getenvInt("JELLYFIN_INVENTORY_SYNC_INTERVAL_SECONDS", cfg.Jellyfin.InventorySync.IntervalSeconds)
	cfg.Jellyfin.InventorySync.MaxRefreshesPerRun = getenvInt("JELLYFIN_INVENTORY_SYNC_MAX_REFRESHES_PER_RUN", cfg.Jellyfin.InventorySync.MaxRefreshesPerRun)
	cfg.Jellyfin.InventorySync.CooldownMinutes = getenvInt("JELLYFIN_INVENTORY_SYNC_COOLDOWN_MINUTES", cfg.Jellyfin.InventorySync.CooldownMinutes)
	cfg.Jellyfin.TimeoutSeconds = getenvInt("JELLYFIN_TIMEOUT_SECONDS", cfg.Jellyfin.TimeoutSeconds)

	cfg.PostgresURL = getenv("POSTGRES_URL", cfg.PostgresURL)
	cfg.LibraryEdition = getenv("LIBRARY_EDITION", cfg.LibraryEdition)
	cfg.Tenant = getenv("TENANT", getenv("TENANT_NAME", cfg.Tenant))

	cfg.RadarrURL = getenv("RADARR_URL", cfg.RadarrURL)
	cfg.RadarrAPIKey = getenv("RADARR_API_KEY", cfg.RadarrAPIKey)

	cfg.SonarrURL = getenv("SONARR_URL", cfg.SonarrURL)
	cfg.SonarrAPIKey = getenv("SONARR_API_KEY", cfg.SonarrAPIKey)

	cfg.SeerrURL = getenv("SEERR_URL", cfg.SeerrURL)
	cfg.SeerrAPIKey = getenv("SEERR_API_KEY", cfg.SeerrAPIKey)
	cfg.SeerrSyncEnabled = getenvBool("SEERR_SYNC_ENABLED", cfg.SeerrSyncEnabled)
	cfg.SeerrSyncIntervalSeconds = getenvInt("SEERR_SYNC_INTERVAL_SECONDS", cfg.SeerrSyncIntervalSeconds)
	cfg.SeerrSyncLimit = getenvInt("SEERR_SYNC_LIMIT", cfg.SeerrSyncLimit)

	cfg.CSIPath = getenv("CSI_PATH", cfg.CSIPath)
	cfg.HealthAddr = getenv("HEALTH_ADDR", cfg.HealthAddr)
	cfg.APIEnabled = getenvBool("API_ENABLED", cfg.APIEnabled)
	cfg.APIToken = getenv("API_TOKEN", cfg.APIToken)
	cfg.APIRequireToken = getenvBool("API_REQUIRE_TOKEN", cfg.APIRequireToken)
	cfg.MetricsEnabled = getenvBool("METRICS_ENABLED", cfg.MetricsEnabled)
	cfg.PlaybackEnabled = getenvBool("PLAYBACK_ENABLED", cfg.PlaybackEnabled)
	cfg.PlaybackRearmOnPlay = getenvBool("PLAYBACK_REARM_ON_PLAY", cfg.PlaybackRearmOnPlay)
	cfg.PlaybackCooldownSeconds = getenvInt("PLAYBACK_COOLDOWN_SECONDS", cfg.PlaybackCooldownSeconds)
	cfg.PlaybackIgnoredTitles = getenvCSV("PLAYBACK_IGNORED_TITLES", cfg.PlaybackIgnoredTitles)
	cfg.PlaybackIgnoredTitleContains = getenvCSV("PLAYBACK_IGNORED_TITLE_CONTAINS", cfg.PlaybackIgnoredTitleContains)
	cfg.SelfHealEnabled = getenvBool("SELF_HEAL_ENABLED", cfg.SelfHealEnabled)
	cfg.SelfHealIntervalSeconds = getenvInt("SELF_HEAL_INTERVAL_SECONDS", cfg.SelfHealIntervalSeconds)
	cfg.SelfHealRefreshAvailable = getenvBool("SELF_HEAL_REFRESH_AVAILABLE", cfg.SelfHealRefreshAvailable)
	cfg.SelfHealRecentHours = getenvInt("SELF_HEAL_RECENT_HOURS", cfg.SelfHealRecentHours)
	cfg.SelfHealMaxRefreshesPerRun = getenvInt("SELF_HEAL_MAX_REFRESHES_PER_RUN", cfg.SelfHealMaxRefreshesPerRun)
	cfg.RadarrSyncEnabled = getenvBool("RADARR_SYNC_ENABLED", cfg.RadarrSyncEnabled)
	cfg.RadarrSyncIntervalSeconds = getenvInt("RADARR_SYNC_INTERVAL_SECONDS", cfg.RadarrSyncIntervalSeconds)
	cfg.SonarrSyncEnabled = getenvBool("SONARR_SYNC_ENABLED", cfg.SonarrSyncEnabled)
	cfg.SonarrSyncIntervalSeconds = getenvInt("SONARR_SYNC_INTERVAL_SECONDS", cfg.SonarrSyncIntervalSeconds)
	cfg.RearmEnabled = getenvBool("REARM_ENABLED", cfg.RearmEnabled)
	cfg.MaxRearmsPerRun = getenvInt("MAX_REARMS_PER_RUN", cfg.MaxRearmsPerRun)

	cfg.ReconcileIntervalSeconds = getenvInt("RECONCILE_INTERVAL_SECONDS", cfg.ReconcileIntervalSeconds)
	cfg.CSIVisibilityTimeoutSeconds = getenvInt("CSI_VISIBILITY_TIMEOUT_SECONDS", cfg.CSIVisibilityTimeoutSeconds)
	cfg.CSIVisibilityPollSeconds = getenvInt("CSI_VISIBILITY_POLL_SECONDS", cfg.CSIVisibilityPollSeconds)
	cfg.CSIVisibilityRetrySeconds = getenvInt("CSI_VISIBILITY_RETRY_SECONDS", cfg.CSIVisibilityRetrySeconds)
	cfg.RcloneRCEnabled = getenvBool("RCLONE_RC_ENABLED", cfg.RcloneRCEnabled)
	cfg.RcloneRCURL = getenv("RCLONE_RC_URL", cfg.RcloneRCURL)
	cfg.RcloneRCUsername = getenv("RCLONE_RC_USERNAME", cfg.RcloneRCUsername)
	cfg.RcloneRCPassword = getenv("RCLONE_RC_PASSWORD", cfg.RcloneRCPassword)
	cfg.RcloneRCRefreshAfterRearm = getenvBool("RCLONE_RC_REFRESH_AFTER_REARM", cfg.RcloneRCRefreshAfterRearm)
	cfg.RcloneRCTimeoutSeconds = getenvInt("RCLONE_RC_TIMEOUT_SECONDS", cfg.RcloneRCTimeoutSeconds)
	cfg.CacheGraceHours = getenvInt("CACHE_GRACE_HOURS", cfg.CacheGraceHours)
	cfg.MaxRetries = getenvInt("MAX_RETRIES", cfg.MaxRetries)
	cfg.ConcurrentWorkers = getenvInt("CONCURRENT_WORKERS", cfg.ConcurrentWorkers)
	cfg.DBAutoMigrate = getenvBool("DB_AUTO_MIGRATE", cfg.DBAutoMigrate)
}

func hydrateDurations(cfg *Config) {
	cfg.ReconcileInterval = time.Duration(cfg.ReconcileIntervalSeconds) * time.Second
	if cfg.CSIVisibilityTimeoutSeconds <= 0 {
		cfg.CSIVisibilityTimeoutSeconds = 900
	}
	if cfg.CSIVisibilityPollSeconds <= 0 {
		cfg.CSIVisibilityPollSeconds = 10
	}
	if cfg.CSIVisibilityRetrySeconds <= 0 {
		cfg.CSIVisibilityRetrySeconds = 60
	}

	if cfg.RcloneRCTimeoutSeconds <= 0 {
		cfg.RcloneRCTimeoutSeconds = 10
	}
	cfg.CSIVisibilityTimeout = time.Duration(cfg.CSIVisibilityTimeoutSeconds) * time.Second
	cfg.CSIVisibilityPoll = time.Duration(cfg.CSIVisibilityPollSeconds) * time.Second
	cfg.CSIVisibilityRetry = time.Duration(cfg.CSIVisibilityRetrySeconds) * time.Second
	cfg.RcloneRCTimeout = time.Duration(cfg.RcloneRCTimeoutSeconds) * time.Second
	cfg.CacheGrace = time.Duration(cfg.CacheGraceHours) * time.Hour
	cfg.RadarrSyncInterval = time.Duration(cfg.RadarrSyncIntervalSeconds) * time.Second
	cfg.SonarrSyncInterval = time.Duration(cfg.SonarrSyncIntervalSeconds) * time.Second
	cfg.SeerrSyncInterval = time.Duration(cfg.SeerrSyncIntervalSeconds) * time.Second
	if cfg.SelfHealIntervalSeconds <= 0 {
		cfg.SelfHealIntervalSeconds = 300
	}
	if cfg.SelfHealRecentHours <= 0 {
		cfg.SelfHealRecentHours = 24
	}
	if cfg.SelfHealMaxRefreshesPerRun <= 0 {
		cfg.SelfHealMaxRefreshesPerRun = 5
	}
	cfg.PlaybackCooldown = time.Duration(cfg.PlaybackCooldownSeconds) * time.Second
	cfg.SelfHealInterval = time.Duration(cfg.SelfHealIntervalSeconds) * time.Second
}

func validate(cfg Config) error {
	if cfg.LibraryEdition != "" && cfg.LibraryEdition != "4k" {
		return errors.New("library_edition must be empty or 4k")
	}
	for _, root := range cfg.LibraryPaths {
		if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
			return errors.New("library_paths must contain absolute library directories, not filesystem root")
		}
	}
	if cfg.LibraryEdition == "4k" && len(cfg.LibraryPaths) == 0 {
		return errors.New("4k controller requires explicit library_paths")
	}
	if cfg.LibraryEdition != "" && cfg.Role != RoleController {
		return errors.New("library_edition requires controller role")
	}

	if cfg.Role != "" && cfg.Role != RoleCombined && cfg.Role != RoleGateway && cfg.Role != RoleController {
		return errors.New("role must be combined, gateway or controller")
	}
	if !cfg.Gateway.Enabled {
		return errors.New("gateway.enabled must be true; legacy backends were removed")
	}

	if cfg.Role != RoleGateway && cfg.Jellyfin.RefreshMode != "" && cfg.Jellyfin.RefreshMode != "notification" && cfg.Jellyfin.RefreshMode != "targeted_scan" {
		return errors.New("jellyfin.refresh_mode must be notification or targeted_scan")
	}
	if err := cfg.Gateway.OIDC.Validate(); err != nil {
		return err
	}
	if cfg.Gateway.OIDC.Enabled && cfg.Gateway.DashboardExternalAuth {
		return fmt.Errorf("choose OIDC or external dashboard auth, not both")
	}
	if cfg.Gateway.Enabled && (cfg.Gateway.ActivePollIntervalSeconds < 15 || cfg.Gateway.ActivePollIntervalSeconds > 3600) {
		return fmt.Errorf("gateway.active_poll_interval_seconds must be between 15 and 3600")
	}

	if cfg.Gateway.Enabled {
		if cfg.Role != RoleController && (cfg.Gateway.QBUsername == "" || cfg.Gateway.QBPassword == "") {
			return errors.New("gateway requires qb_username/qb_password")
		}
	}
	if cfg.Gateway.Enabled {
		for _, category := range cfg.Gateway.Categories() {
			if category == "" || category == "." || category == ".." || strings.ContainsAny(category, "/\\\x00") {
				return errors.New("gateway categories must be nonempty path components")
			}
		}
	}
	if cfg.Gateway.Enabled {
		u := cfg.Gateway.GatewayStorageConfig
		if !cfg.APIEnabled || (cfg.Role != RoleController && (u.APIKey == "" || u.SABAPIKey == "" || u.DAVUsername == "" || u.DAVPassword == "")) || u.SubmitIntervalSeconds < 60 || !strings.HasPrefix(u.MountPath, "/") || !strings.HasPrefix(u.DownloadPath, "/") || u.MountPath == "/" || u.DownloadPath == "/" || u.DownloadPath == u.MountPath || strings.HasPrefix(filepath.Clean(u.DownloadPath), filepath.Clean(u.MountPath)+"/") || strings.HasPrefix(filepath.Clean(u.MountPath), filepath.Clean(u.DownloadPath)+"/") {
			return errors.New("gateway requires API enabled, TorBox/SAB keys, DAV credentials, distinct absolute mount/download paths and submit_interval_seconds >=60")
		}
	}

	if cfg.Role != RoleGateway && cfg.Jellyfin.Enabled && (strings.TrimSpace(cfg.Jellyfin.URL) == "" || strings.TrimSpace(cfg.Jellyfin.APIKey) == "") {
		return errors.New("jellyfin.url and jellyfin.api_key are required when Jellyfin is enabled")
	}
	if cfg.Role != RoleGateway && cfg.Jellyfin.InventorySync.Enabled && (cfg.Jellyfin.InventorySync.IntervalSeconds < 60 || cfg.Jellyfin.InventorySync.MaxRefreshesPerRun < 1 || cfg.Jellyfin.InventorySync.MaxRefreshesPerRun > 100 || cfg.Jellyfin.InventorySync.CooldownMinutes < 1) {
		return errors.New("jellyfin.inventory_sync requires interval_seconds >=60, max_refreshes_per_run 1..100 and cooldown_minutes >=1")
	}
	if cfg.Role != RoleGateway && (cfg.Jellyfin.RefreshDelaySeconds < 0 || cfg.Jellyfin.TimeoutSeconds <= 0) {
		return errors.New("jellyfin refresh_delay_seconds must be nonnegative and timeout_seconds must be positive")
	}

	if cfg.PostgresURL == "" {
		return errors.New("POSTGRES_URL or postgres_url is required")
	}
	if cfg.Tenant == "" {
		return errors.New("TENANT/TENANT_NAME or tenant is required")
	}
	if cfg.Role != RoleGateway && (cfg.RadarrURL == "" || cfg.RadarrAPIKey == "") {
		return errors.New("RADARR_URL/RADARR_API_KEY or radarr.url/radarr.api_key are required")
	}
	if cfg.Role != RoleGateway && cfg.SonarrSyncEnabled && (cfg.SonarrURL == "" || cfg.SonarrAPIKey == "") {
		return errors.New("SONARR_URL/SONARR_API_KEY or sonarr.url/sonarr.api_key are required when Sonarr sync is enabled")
	}
	if cfg.Role != RoleGateway && cfg.SeerrSyncEnabled && (cfg.SeerrURL == "" || cfg.SeerrAPIKey == "") {
		return errors.New("SEERR_URL/SEERR_API_KEY or seerr.url/seerr.api_key are required when Seerr sync is enabled")
	}

	if cfg.Role != RoleGateway && cfg.CSIPath == "" {
		return errors.New("CSI_PATH or csi_path is required")
	}
	if cfg.HealthAddr == "" {
		return errors.New("HEALTH_ADDR or health_addr is required")
	}
	if cfg.Role != RoleGateway && cfg.APIEnabled && cfg.APIRequireToken && cfg.APIToken == "" {
		return errors.New("API_TOKEN is required when API_REQUIRE_TOKEN=true")
	}
	if cfg.Role != RoleGateway && cfg.RcloneRCEnabled && cfg.RcloneRCURL == "" {
		return errors.New("RCLONE_RC_URL or rclone_rc.url is required when RCLONE_RC_ENABLED=true")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvCSV(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	parts := strings.Split(raw, ",")
	return normalizeStringList(parts)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func getenvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

// StateTenant isolates Arr identity while the gateway retains the real tenant.
// The standard scope is unchanged for existing installations.
func (cfg Config) StateTenant() string {
	if cfg.LibraryEdition == "4k" {
		return cfg.Tenant + ":4k"
	}
	return cfg.Tenant
}
