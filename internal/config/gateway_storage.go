package config

type GatewayStorageConfig struct {
	RecoverMissing        bool   `yaml:"recover_missing"`
	Enabled               bool   `yaml:"enabled"`
	APIKey                string `yaml:"api_key"`
	SABAPIKey             string `yaml:"sab_api_key"`
	DAVUsername           string `yaml:"dav_username"`
	DAVPassword           string `yaml:"dav_password"`
	MountPath             string `yaml:"mount_path"`
	DownloadPath          string `yaml:"download_path"`
	SubmitIntervalSeconds int    `yaml:"submit_interval_seconds"`
}

func DefaultGatewayStorage() GatewayStorageConfig {
	return GatewayStorageConfig{RecoverMissing: true, MountPath: "/storage/torbox", DownloadPath: "/storage/media/downloads/rehydrator", SubmitIntervalSeconds: 120}
}
