package config

import (
    "errors"
    "os"
    "strconv"
    "time"
)

type Config struct {
    PostgresURL string

    RadarrURL    string
    RadarrAPIKey string

    SonarrURL    string
    SonarrAPIKey string

    TorBoxAPIKey string

    CSIPath string

    ReconcileInterval time.Duration
    CSIWait           time.Duration
    CacheGrace        time.Duration
    MaxRetries        int
    ConcurrentWorkers int
    DBAutoMigrate     bool
}

func Load() (Config, error) {
    cfg := Config{
        PostgresURL: os.Getenv("POSTGRES_URL"),

        RadarrURL:    os.Getenv("RADARR_URL"),
        RadarrAPIKey: os.Getenv("RADARR_API_KEY"),

        SonarrURL:    os.Getenv("SONARR_URL"),
        SonarrAPIKey: os.Getenv("SONARR_API_KEY"),

        TorBoxAPIKey: os.Getenv("TORBOX_API_KEY"),

        CSIPath: getenv("CSI_PATH", "/storage/media"),

        ReconcileInterval: time.Duration(getenvInt("RECONCILE_INTERVAL_SECONDS", 30)) * time.Second,
        CSIWait:           time.Duration(getenvInt("CSI_WAIT_SECONDS", 180)) * time.Second,
        CacheGrace:        time.Duration(getenvInt("CACHE_GRACE_HOURS", 24)) * time.Hour,
        MaxRetries:        getenvInt("MAX_RETRIES", 10),
        ConcurrentWorkers: getenvInt("CONCURRENT_WORKERS", 4),
        DBAutoMigrate:     getenvBool("DB_AUTO_MIGRATE", false),
    }

    if cfg.PostgresURL == "" {
        return cfg, errors.New("POSTGRES_URL is required")
    }
    if cfg.RadarrURL == "" || cfg.RadarrAPIKey == "" {
        return cfg, errors.New("RADARR_URL and RADARR_API_KEY are required")
    }
    if cfg.TorBoxAPIKey == "" {
        return cfg, errors.New("TORBOX_API_KEY is required")
    }

    return cfg, nil
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
