package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

const DefaultConfigYAML = `postgres_url: ""
tenant: ""

radarr:
  url: http://radarr:7878
  api_key: ""

sonarr:
  url: http://sonarr:8989
  api_key: ""

seerr:
  url: http://seerr:5055
  api_key: ""
  sync:
    enabled: false
    interval_seconds: 300
    limit: 100

decypharr:
  url: http://decypharr:8282
  username: ""
  password: ""
  radarr_category: radarr
  sonarr_category: sonarr
  delete_files_on_prune: true

# Re-arm/add goes through Decypharr. Prune/delete goes directly to TorBox by infohash.
torbox:
  api_key: ""

csi_path: /storage/media
health_addr: ":8080"

api:
  enabled: true
  token: ""

radarr_sync:
  enabled: true
  interval_seconds: 300

# Prune success is provider-authoritative by default. CSI/rclone can show stale paths.
prune_wait_for_csi_gone: false
# When false, ARCHIVED+rearm_requested always queues Decypharr even if CSI still shows the library path.
rearm_short_circuit_if_csi_visible: false

reconcile_interval_seconds: 30
csi_wait_seconds: 300
cache_grace_hours: 24
max_retries: 10
concurrent_workers: 4
db_auto_migrate: false
`

type Config struct {
	PostgresURL string
	Tenant      string

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

	DecypharrURL                string
	DecypharrUsername           string
	DecypharrPassword           string
	DecypharrRadarrCategory     string
	DecypharrSonarrCategory     string
	DecypharrDeleteFilesOnPrune bool

	// Used for prune/dehydrate. Re-arm/add still goes through Decypharr.
	TorBoxAPIKey string

	CSIPath    string
	HealthAddr string

	APIEnabled bool
	APIToken   string

	RadarrSyncEnabled         bool
	RadarrSyncIntervalSeconds int
	RadarrSyncInterval        time.Duration

	PruneWaitForCSIGone           bool
	RearmShortCircuitIfCSIVisible bool

	ReconcileIntervalSeconds int
	CSIWaitSeconds           int
	CacheGraceHours          int

	ReconcileInterval time.Duration
	CSIWait           time.Duration
	CacheGrace        time.Duration

	MaxRetries        int
	ConcurrentWorkers int
	DBAutoMigrate     bool

	ConfigPath    string
	ConfigCreated bool
}

type fileConfig struct {
	PostgresURL string `yaml:"postgres_url"`
	Tenant      string `yaml:"tenant"`

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

	Decypharr struct {
		URL                string `yaml:"url"`
		Username           string `yaml:"username"`
		Password           string `yaml:"password"`
		RadarrCategory     string `yaml:"radarr_category"`
		SonarrCategory     string `yaml:"sonarr_category"`
		DeleteFilesOnPrune *bool  `yaml:"delete_files_on_prune"`
	} `yaml:"decypharr"`

	TorBox struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"torbox"`

	CSIPath    string `yaml:"csi_path"`
	HealthAddr string `yaml:"health_addr"`

	API struct {
		Enabled *bool  `yaml:"enabled"`
		Token   string `yaml:"token"`
	} `yaml:"api"`

	RadarrSync struct {
		Enabled         *bool `yaml:"enabled"`
		IntervalSeconds int   `yaml:"interval_seconds"`
	} `yaml:"radarr_sync"`

	PruneWaitForCSIGone           bool `yaml:"prune_wait_for_csi_gone"`
	RearmShortCircuitIfCSIVisible bool `yaml:"rearm_short_circuit_if_csi_visible"`

	ReconcileIntervalSeconds int  `yaml:"reconcile_interval_seconds"`
	CSIWaitSeconds           int  `yaml:"csi_wait_seconds"`
	CacheGraceHours          int  `yaml:"cache_grace_hours"`
	MaxRetries               int  `yaml:"max_retries"`
	ConcurrentWorkers        int  `yaml:"concurrent_workers"`
	DBAutoMigrate            bool `yaml:"db_auto_migrate"`
}

func Load(configPath string) (Config, error) {
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

	applyEnvOverrides(&cfg)
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
		RadarrURL:                     "http://radarr:7878",
		SonarrURL:                     "http://sonarr:8989",
		DecypharrURL:                  "http://decypharr:8282",
		DecypharrRadarrCategory:       "radarr",
		DecypharrSonarrCategory:       "sonarr",
		DecypharrDeleteFilesOnPrune:   true,
		CSIPath:                       "/storage/media",
		HealthAddr:                    ":8080",
		APIEnabled:                    true,
		RadarrSyncEnabled:             true,
		RadarrSyncIntervalSeconds:     300,
		SeerrURL:                      "http://seerr:5055",
		SeerrSyncEnabled:              false,
		SeerrSyncIntervalSeconds:      300,
		SeerrSyncLimit:                100,
		PruneWaitForCSIGone:           false,
		RearmShortCircuitIfCSIVisible: false,
		ReconcileIntervalSeconds:      30,
		CSIWaitSeconds:                300,
		CacheGraceHours:               24,
		MaxRetries:                    10,
		ConcurrentWorkers:             4,
		DBAutoMigrate:                 false,
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
	var fc fileConfig

	data, err := os.ReadFile(path)
	if err != nil {
		return fc, err
	}

	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fc, err
	}

	return fc, nil
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.PostgresURL != "" {
		cfg.PostgresURL = fc.PostgresURL
	}
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

	if fc.Decypharr.URL != "" {
		cfg.DecypharrURL = fc.Decypharr.URL
	}
	if fc.Decypharr.Username != "" {
		cfg.DecypharrUsername = fc.Decypharr.Username
	}
	if fc.Decypharr.Password != "" {
		cfg.DecypharrPassword = fc.Decypharr.Password
	}
	if fc.Decypharr.RadarrCategory != "" {
		cfg.DecypharrRadarrCategory = fc.Decypharr.RadarrCategory
	}
	if fc.Decypharr.SonarrCategory != "" {
		cfg.DecypharrSonarrCategory = fc.Decypharr.SonarrCategory
	}
	if fc.Decypharr.DeleteFilesOnPrune != nil {
		cfg.DecypharrDeleteFilesOnPrune = *fc.Decypharr.DeleteFilesOnPrune
	}

	if fc.TorBox.APIKey != "" {
		cfg.TorBoxAPIKey = fc.TorBox.APIKey
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
	if fc.RadarrSync.Enabled != nil {
		cfg.RadarrSyncEnabled = *fc.RadarrSync.Enabled
	}
	if fc.RadarrSync.IntervalSeconds > 0 {
		cfg.RadarrSyncIntervalSeconds = fc.RadarrSync.IntervalSeconds
	}
	cfg.PruneWaitForCSIGone = fc.PruneWaitForCSIGone
	cfg.RearmShortCircuitIfCSIVisible = fc.RearmShortCircuitIfCSIVisible
	if fc.ReconcileIntervalSeconds > 0 {
		cfg.ReconcileIntervalSeconds = fc.ReconcileIntervalSeconds
	}
	if fc.CSIWaitSeconds > 0 {
		cfg.CSIWaitSeconds = fc.CSIWaitSeconds
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
	cfg.PostgresURL = getenv("POSTGRES_URL", cfg.PostgresURL)
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

	cfg.DecypharrURL = getenv("DECYPHARR_URL", cfg.DecypharrURL)
	cfg.DecypharrUsername = getenv("DECYPHARR_USERNAME", cfg.DecypharrUsername)
	cfg.DecypharrPassword = getenv("DECYPHARR_PASSWORD", cfg.DecypharrPassword)
	cfg.DecypharrRadarrCategory = getenv("DECYPHARR_RADARR_CATEGORY", cfg.DecypharrRadarrCategory)
	cfg.DecypharrSonarrCategory = getenv("DECYPHARR_SONARR_CATEGORY", cfg.DecypharrSonarrCategory)
	cfg.DecypharrDeleteFilesOnPrune = getenvBool("DECYPHARR_DELETE_FILES_ON_PRUNE", cfg.DecypharrDeleteFilesOnPrune)

	cfg.TorBoxAPIKey = getenv("TORBOX_API_KEY", cfg.TorBoxAPIKey)

	cfg.CSIPath = getenv("CSI_PATH", cfg.CSIPath)
	cfg.HealthAddr = getenv("HEALTH_ADDR", cfg.HealthAddr)
	cfg.APIEnabled = getenvBool("API_ENABLED", cfg.APIEnabled)
	cfg.APIToken = getenv("API_TOKEN", cfg.APIToken)
	cfg.RadarrSyncEnabled = getenvBool("RADARR_SYNC_ENABLED", cfg.RadarrSyncEnabled)
	cfg.RadarrSyncIntervalSeconds = getenvInt("RADARR_SYNC_INTERVAL_SECONDS", cfg.RadarrSyncIntervalSeconds)
	cfg.PruneWaitForCSIGone = getenvBool("PRUNE_WAIT_FOR_CSI_GONE", cfg.PruneWaitForCSIGone)
	cfg.RearmShortCircuitIfCSIVisible = getenvBool("REARM_SHORT_CIRCUIT_IF_CSI_VISIBLE", cfg.RearmShortCircuitIfCSIVisible)

	cfg.ReconcileIntervalSeconds = getenvInt("RECONCILE_INTERVAL_SECONDS", cfg.ReconcileIntervalSeconds)
	cfg.CSIWaitSeconds = getenvInt("CSI_WAIT_SECONDS", cfg.CSIWaitSeconds)
	cfg.CacheGraceHours = getenvInt("CACHE_GRACE_HOURS", cfg.CacheGraceHours)
	cfg.MaxRetries = getenvInt("MAX_RETRIES", cfg.MaxRetries)
	cfg.ConcurrentWorkers = getenvInt("CONCURRENT_WORKERS", cfg.ConcurrentWorkers)
	cfg.DBAutoMigrate = getenvBool("DB_AUTO_MIGRATE", cfg.DBAutoMigrate)
}

func hydrateDurations(cfg *Config) {
	cfg.ReconcileInterval = time.Duration(cfg.ReconcileIntervalSeconds) * time.Second
	cfg.CSIWait = time.Duration(cfg.CSIWaitSeconds) * time.Second
	cfg.CacheGrace = time.Duration(cfg.CacheGraceHours) * time.Hour
	cfg.RadarrSyncInterval = time.Duration(cfg.RadarrSyncIntervalSeconds) * time.Second
	cfg.SeerrSyncInterval = time.Duration(cfg.SeerrSyncIntervalSeconds) * time.Second
}

func validate(cfg Config) error {
	if cfg.PostgresURL == "" {
		return errors.New("POSTGRES_URL or postgres_url is required")
	}
	if cfg.Tenant == "" {
		return errors.New("TENANT/TENANT_NAME or tenant is required")
	}
	if cfg.RadarrURL == "" || cfg.RadarrAPIKey == "" {
		return errors.New("RADARR_URL/RADARR_API_KEY or radarr.url/radarr.api_key are required")
	}
	if cfg.SeerrSyncEnabled && (cfg.SeerrURL == "" || cfg.SeerrAPIKey == "") {
		return errors.New("SEERR_URL/SEERR_API_KEY or seerr.url/seerr.api_key are required when Seerr sync is enabled")
	}
	if cfg.DecypharrURL == "" {
		return errors.New("DECYPHARR_URL or decypharr.url is required")
	}
	if cfg.DecypharrUsername != "" && cfg.DecypharrPassword == "" {
		return errors.New("DECYPHARR_PASSWORD is required when DECYPHARR_USERNAME is set")
	}
	if cfg.TorBoxAPIKey == "" {
		return errors.New("TORBOX_API_KEY or torbox.api_key is required for prune/delete")
	}
	if cfg.CSIPath == "" {
		return errors.New("CSI_PATH or csi_path is required")
	}
	if cfg.HealthAddr == "" {
		return errors.New("HEALTH_ADDR or health_addr is required")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
