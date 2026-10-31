// Package cloudusenet retains the initial cloud-NZB API while its implementation
// lives in the protocol-independent gateway package.
package cloudusenet

import (
	"github.com/raefon/rehydrator/internal/config"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/gateway"
)

type Service = gateway.Service
type Provider = gateway.Provider

const Prefix = gateway.Prefix
const SABPath = gateway.SABPath
const DAVPath = gateway.DAVPath

func IsJob(id string) bool { return gateway.IsJob(id) }
func New(cfg config.CloudUsenetConfig, tenant string, repo *db.Repo, provider Provider, categories ...string) *Service {
	return gateway.New(cfg, tenant, repo, provider, categories...)
}
