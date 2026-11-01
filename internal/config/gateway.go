package config

import "github.com/raefon/rehydrator/internal/dashboardauth"

// GatewayConfig enables the direct, shared TorBox torrent and Usenet gateway.
type GatewayConfig struct {
	CloudUsenetConfig     `yaml:",inline"`
	OIDC                  dashboardauth.Config `yaml:"oidc"`
	DashboardExternalAuth bool                 `yaml:"dashboard_external_auth"`
	QBUsername            string               `yaml:"qb_username"`
	QBPassword            string               `yaml:"qb_password"`
}

func DefaultGateway() GatewayConfig {
	cfg := DefaultCloudUsenet()
	cfg.MountPath = "/storage/torbox"
	cfg.DownloadPath = "/storage/media/downloads/rehydrator"
	return GatewayConfig{CloudUsenetConfig: cfg}
}
