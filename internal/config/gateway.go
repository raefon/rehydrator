package config

import "github.com/raefon/rehydrator/internal/dashboardauth"

// GatewayConfig enables the direct, shared TorBox torrent and Usenet gateway.
type GatewayConfig struct {
	RadarrCategory            string `yaml:"radarr_category"`
	SonarrCategory            string `yaml:"sonarr_category"`
	ActivePollIntervalSeconds int    `yaml:"active_poll_interval_seconds"`
	GatewayStorageConfig      `yaml:",inline"`
	OIDC                      dashboardauth.Config `yaml:"oidc"`
	DashboardExternalAuth     bool                 `yaml:"dashboard_external_auth"`
	QBUsername                string               `yaml:"qb_username"`
	QBPassword                string               `yaml:"qb_password"`
}

func DefaultGateway() GatewayConfig {
	cfg := DefaultGatewayStorage()
	cfg.Enabled = true
	cfg.MountPath = "/storage/torbox"
	cfg.DownloadPath = "/storage/media/downloads/rehydrator"
	return GatewayConfig{GatewayStorageConfig: cfg, RadarrCategory: "radarr", SonarrCategory: "sonarr", ActivePollIntervalSeconds: 30}
}
