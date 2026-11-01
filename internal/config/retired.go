package config

import (
	"fmt"
	"os"
	"strings"
)

// Environment variables otherwise silently ignored are rejected during upgrade.
func rejectRetiredEnvironment() error {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if value == "" {
			continue
		}
		retired := strings.HasPrefix(name, "DECYPHARR_") || strings.HasPrefix(name, "CLOUD_USENET_") || strings.HasPrefix(name, "TORBOX_HEALTH_") || (strings.HasPrefix(name, "FILE_HEALTH_") && name != "FILE_HEALTH_CANARY_PATH")
		switch name {
		case "REARM_BACKEND", "PRUNE_ENABLED", "MAX_PRUNES_PER_RUN", "PRUNE_WAIT_FOR_CSI_GONE", "REARM_SHORT_CIRCUIT_IF_CSI_VISIBLE", "CSI_WAIT_SECONDS", "PROVIDER_COOLDOWN_SECONDS", "JELLYFIN_REFRESH_AFTER_PRUNE":
			retired = true
		}
		if retired {
			return fmt.Errorf("%s was removed; migrate to the gateway/controller config examples (docs/configuration.md)", name)
		}
	}
	return nil
}
