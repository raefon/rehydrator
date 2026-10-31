package config

type CloudUsenetConfig struct {
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

func DefaultCloudUsenet() CloudUsenetConfig {
	return CloudUsenetConfig{RecoverMissing: true, MountPath: "/storage/torbox-cloud", DownloadPath: "/storage/media/downloads/torbox-usenet", SubmitIntervalSeconds: 120}
}
