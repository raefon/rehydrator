package uilogs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Store receives only sanitized UI snapshots, never raw stdout.
type Store interface {
	PublishLogs(context.Context, string, string, string, []Entry) error
}

// Publish runs outside the logging path so SQL never blocks log output.
func Publish(ctx context.Context, store Store, tenant, source string) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return
	}
	instance := hex.EncodeToString(id[:])
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		rows := Recent.Snapshot()
		for i := range rows {
			rows[i].Source = source
		}
		publishCtx, cancel := context.WithTimeout(ctx, time.Second)
		// Avoid recursively logging database failures.
		_ = store.PublishLogs(publishCtx, tenant, instance, source, rows)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
