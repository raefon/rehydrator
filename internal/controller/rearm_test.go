package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/raefon/rehydrator/internal/model"
)

func TestHashOnlyAndLegacyMagnetRequireRecovery(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef01234567"
	magnet := "magnet:?xt=urn:btih:" + hash
	for _, mediaType := range []model.MediaType{model.MediaMovie, model.MediaSeries} {
		for _, withMagnet := range []bool{false, true} {
			item := model.MediaCacheState{InfoHash: &hash, MediaType: mediaType}
			if withMagnet {
				item.Magnet = &magnet
			}
			c := New(Options{})
			if _, err := c.resolveRearmTorrent(context.Background(), item); !errors.Is(err, model.ErrSourceRecoveryRequired) {
				t.Fatalf("unproven legacy source accepted: %v", err)
			}
		}
	}
}
