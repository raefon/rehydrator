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

decypharr:
  url: http://decypharr:8282
  username: ""
  password: ""
  radarr_category: radarr
  sonarr_category: sonarr
  delete_files_on_prune: true

# Deprecated/legacy. Decypharr is now the primary download-client integration.
torbox:
  api_key: ""

csi_path: /storage/media
health_addr: ":8080"

reconcile_interval_seconds: 30
csi_wait_seconds: 300
cache_grace_hours: 24
max_retries: 10
concurrent_workers: 4
db_auto_migrate: false
`

type Config struct {
	PostgresURL string

	RadarrURL    string
	RadarrAPIKey string

	SonarrURL    string
	SonarrAPIKey string

	DecypharrURL                string
	DecypharrUsername           string
	DecypharrPassword           string
	DecypharrRadarrCategory     string
	DecypharrSonarrCategory     string
	DecypharrDeleteFilesOnPrune bool

	// Deprecated/legacy fallback only.
	TorBoxAPIKey string

	CSIPath    string
	HealthAddr string

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

	Radarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"radarr"`

	Sonarr struct {
		URL    string `yaml:"url"`
		APIKey string `yaml:"api_key"`
	} `yaml:"sonarr"`

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
		RadarrURL:                   "http://radarr:7878",
		SonarrURL:                   "http://sonarr:8989",
		DecypharrURL:                "http://decypharr:8282",
		DecypharrRadarrCategory:     "radarr",
		DecypharrSonarrCategory:     "sonarr",
		DecypharrDeleteFilesOnPrune: true,
		CSIPath:                     "/storage/media",
		HealthAddr:                  ":8080",
		ReconcileIntervalSeconds:    30,
		CSIWaitSeconds:              300,
		CacheGraceHours:             24,
		MaxRetries:                  10,
		ConcurrentWorkers:           4,
		DBAutoMigrate:               false,
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

	cfg.RadarrURL = getenv("RADARR_URL", cfg.RadarrURL)
	cfg.RadarrAPIKey = getenv("RADARR_API_KEY", cfg.RadarrAPIKey)

	cfg.SonarrURL = getenv("SONARR_URL", cfg.SonarrURL)
	cfg.SonarrAPIKey = getenv("SONARR_API_KEY", cfg.SonarrAPIKey)

	cfg.DecypharrURL = getenv("DECYPHARR_URL", cfg.DecypharrURL)
	cfg.DecypharrUsername = getenv("DECYPHARR_USERNAME", cfg.DecypharrUsername)
	cfg.DecypharrPassword = getenv("DECYPHARR_PASSWORD", cfg.DecypharrPassword)
	cfg.DecypharrRadarrCategory = getenv("DECYPHARR_RADARR_CATEGORY", cfg.DecypharrRadarrCategory)
	cfg.DecypharrSonarrCategory = getenv("DECYPHARR_SONARR_CATEGORY", cfg.DecypharrSonarrCategory)
	cfg.DecypharrDeleteFilesOnPrune = getenvBool("DECYPHARR_DELETE_FILES_ON_PRUNE", cfg.DecypharrDeleteFilesOnPrune)

	cfg.TorBoxAPIKey = getenv("TORBOX_API_KEY", cfg.TorBoxAPIKey)

	cfg.CSIPath = getenv("CSI_PATH", cfg.CSIPath)
	cfg.HealthAddr = getenv("HEALTH_ADDR", cfg.HealthAddr)

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
}

func validate(cfg Config) error {
	if cfg.PostgresURL == "" {
		return errors.New("POSTGRES_URL or postgres_url is required")
	}
	if cfg.RadarrURL == "" || cfg.RadarrAPIKey == "" {
		return errors.New("RADARR_URL/RADARR_API_KEY or radarr.url/radarr.api_key are required")
	}
	if cfg.DecypharrURL == "" {
		return errors.New("DECYPHARR_URL or decypharr.url is required")
	}
	if cfg.DecypharrUsername != "" && cfg.DecypharrPassword == "" {
		return errors.New("DECYPHARR_PASSWORD is required when DECYPHARR_USERNAME is set")
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
