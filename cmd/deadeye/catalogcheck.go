package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

const catalogCheckTTLSeconds = 24 * 60 * 60
const catalogHostedURL = "https://deepaksinghcs14.github.io/deadeye-cc/catalog.json"
const catalogMaxBodyBytes = 1 << 20 // 1MB -- a price table is a few hundred bytes; anything past this is not one

// catalogRefreshEnabled gates the background network refresh, same
// discipline as updateRefreshEnabled: only runDaemon() sets it true, so
// unit tests that call the decision functions directly stay offline.
var catalogRefreshEnabled bool

// catalogCheckState is ~/.deadeye/catalog-check.json: just the TTL
// bookkeeping, kept in its OWN file deliberately separate from
// ~/.deadeye/catalog-cache.json (meta.CatalogCachePath()). That second
// file is a bare catalog.Catalog -- the exact shape catalog.Load() reads,
// with no wrapper -- so Load stays a plain, network-free file read with
// no knowledge of refresh timing.
type catalogCheckState struct {
	CheckedUnix int64 `json:"checked_unix"`
}

func loadCatalogCheckState() catalogCheckState {
	var s catalogCheckState
	if b, err := os.ReadFile(meta.CatalogCheckPath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveCatalogCheckState(s catalogCheckState) {
	b, _ := json.Marshal(s)
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	_ = os.WriteFile(meta.CatalogCheckPath(), append(b, '\n'), 0o600)
}

// maybeRefreshCatalog kicks a background fetch of the hosted catalog if
// mode.catalog_check is on and the TTL has elapsed. Never blocks the
// caller and never touches the hot Load() path -- the refreshed file
// takes effect on the NEXT daemon start, same as the update check.
func maybeRefreshCatalog(cfg config.Config) {
	if cfg.Mode.CatalogCheck == "off" || !catalogRefreshEnabled {
		return
	}
	if nowUnix()-loadCatalogCheckState().CheckedUnix > catalogCheckTTLSeconds {
		go refreshCatalogCache()
	}
}

// refreshCatalogCache fetches the hosted catalog and, only if it passes
// catalog.Valid(), overwrites ~/.deadeye/catalog-cache.json with it -- a
// bad or unreachable publish leaves whatever was cached before untouched,
// never half-applies. CheckedUnix is stamped regardless of outcome so a
// flaky network or a malformed publish doesn't retry every daemon start.
func refreshCatalogCache() {
	saveCatalogCheckState(catalogCheckState{CheckedUnix: nowUnix()})
	fetched, ok := fetchHostedCatalog()
	if !ok {
		return
	}
	b, err := json.Marshal(fetched)
	if err != nil {
		return
	}
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	_ = os.WriteFile(meta.CatalogCachePath(), append(b, '\n'), 0o600)
}

func fetchHostedCatalog() (catalog.Catalog, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(catalogHostedURL)
	if err != nil {
		return catalog.Catalog{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return catalog.Catalog{}, false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBodyBytes))
	if err != nil {
		return catalog.Catalog{}, false
	}
	var c catalog.Catalog
	if json.Unmarshal(b, &c) != nil || !c.Valid() {
		return catalog.Catalog{}, false
	}
	return c, true
}
