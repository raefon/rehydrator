package gateway

import (
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"path/filepath"
	"strings"
)

func validateUpload(data []byte, name string) (model.TorrentMetadata, error) {
	name = filepath.Base(name)
	if name == "." || strings.ContainsAny(name, "\\\x00\r\n") || !strings.HasSuffix(strings.ToLower(name), ".nzb") {
		return model.TorrentMetadata{}, fmt.Errorf("NZB filename required")
	}
	return (model.TorrentMetadata{Protocol: model.ProtocolUsenet, Kind: model.SourceNZBFile, NZBFile: data, NZBFilename: name}).ValidateSource()
}
