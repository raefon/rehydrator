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

radarr:
  url: http://radarr:7878
  api_key: ""

sonarr:
  url: http://sonarr:8989
  api_key: ""

torbox:
  api_key: ""

csi_path: /storage/media

reconcile_interval_seconds: 30
csi_wait_seconds: 180
cache_grace_hours: 24
max_retries: 10
concurrent_workers: 4
db_auto_migrate: false
health_addr: ":8080"
`

type Config struct {
	PostgresURL string

	RadarrURL    string
	RadarrAPIKey string

	SonarrURL    string
	SonarrAPIKey string

	TorBoxAPIKey string

	CSIPath string

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

	HealthAddr string
}

type fileConfig struct {
	PostgresURL string `yaml:"postgres_url"`

	Radarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"radarr"`

	Sonarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"sonarr"`

	TorBox struct {
		APIKey string `yaml:"api_key"`
	} `yaml:"torbox"`

	CSIPath string `yaml:"csi_path"`

	ReconcileIntervalSeconds int  `yaml:"reconcile_interval_seconds"`
	CSIWaitSeconds           int  `yaml:"csi_wait_seconds"`
	CacheGraceHours          int  `yaml:"cache_grace_hours"`
	MaxRetries               int  `yaml:"max_retries"`
	ConcurrentWorkers        int  `yaml:"concurrent_workers"`
	DBAutoMigrate            bool `yaml:"db_auto_migrate"`

	HealthAddr string `yaml:"health_addr"`
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
		CSIPath:                  "/storage/media",
		ReconcileIntervalSeconds: 30,
		CSIWaitSeconds:           180,
		CacheGraceHours:          24,
		MaxRetries:               10,
		ConcurrentWorkers:        4,
		DBAutoMigrate:            false,
		HealthAddr:               ":8080",
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
	cfg.PostgresURL = fc.PostgresURL

	cfg.RadarrURL = fc.Radarr.URL
	cfg.RadarrAPIKey = fc.Radarr.APIKey

	cfg.SonarrURL = fc.Sonarr.URL
	cfg.SonarrAPIKey = fc.Sonarr.APIKey

	cfg.TorBoxAPIKey = fc.TorBox.APIKey

	if fc.CSIPath != "" {
		cfg.CSIPath = fc.CSIPath
	}
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
	if fc.HealthAddr != "" {
		cfg.HealthAddr = fc.HealthAddr
	}

	cfg.DBAutoMigrate = fc.DBAutoMigrate
}

func applyEnvOverrides(cfg *Config) {
	cfg.PostgresURL = getenv("POSTGRES_URL", cfg.PostgresURL)

	cfg.RadarrURL = getenv("RADARR_URL", cfg.RadarrURL)
	cfg.RadarrAPIKey = getenv("RADARR_API_KEY", cfg.RadarrAPIKey)

	cfg.SonarrURL = getenv("SONARR_URL", cfg.SonarrURL)
	cfg.SonarrAPIKey = getenv("SONARR_API_KEY", cfg.SonarrAPIKey)

	cfg.TorBoxAPIKey = getenv("TORBOX_API_KEY", cfg.TorBoxAPIKey)

	cfg.CSIPath = getenv("CSI_PATH", cfg.CSIPath)

	cfg.ReconcileIntervalSeconds = getenvInt("RECONCILE_INTERVAL_SECONDS", cfg.ReconcileIntervalSeconds)
	cfg.CSIWaitSeconds = getenvInt("CSI_WAIT_SECONDS", cfg.CSIWaitSeconds)
	cfg.CacheGraceHours = getenvInt("CACHE_GRACE_HOURS", cfg.CacheGraceHours)
	cfg.MaxRetries = getenvInt("MAX_RETRIES", cfg.MaxRetries)
	cfg.ConcurrentWorkers = getenvInt("CONCURRENT_WORKERS", cfg.ConcurrentWorkers)
	cfg.DBAutoMigrate = getenvBool("DB_AUTO_MIGRATE", cfg.DBAutoMigrate)
	cfg.HealthAddr = getenv("HEALTH_ADDR", cfg.HealthAddr)
}

func hydrateDurations(cfg *Config) {
	cfg.ReconcileInterval = time.Duration(cfg.ReconcileIntervalSeconds) * time.Second
	cfg.CSIWait = time.Duration(cfg.CSIWaitSeconds) * time.Second
	cfg.CacheGrace = time.Duration(cfg.CacheGraceHours) * time.Hour
}

func validate(cfg Config) error {
	if cfg.PostgresURL == "" {
		return errors.New("POSTGRES_URL or postgres_url is required")
	}
	if cfg.RadarrURL == "" || cfg.RadarrAPIKey == "" {
		return errors.New("RADARR_URL/RADARR_API_KEY or radarr.url/radarr.api_key are required")
	}
	if cfg.TorBoxAPIKey == "" {
		return errors.New("TORBOX_API_KEY or torbox.api_key is required")
	}
	return nil
}

func getenv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}

	return i
}

func getenvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}

	return b
}
