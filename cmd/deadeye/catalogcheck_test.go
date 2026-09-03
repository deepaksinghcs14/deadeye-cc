package main

import (
	"os"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// TestCatalogRefreshDisabledOutsideDaemon: catalogRefreshEnabled defaults
// false (only runDaemon() flips it), so maybeRefreshCatalog is a no-op
// under test even with catalog_check=on and a stale/absent cache -- the
// same offline-by-default guarantee updatecheck_test.go proves for the
// update check.
func TestCatalogRefreshDisabledOutsideDaemon(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if catalogRefreshEnabled {
		t.Fatal("catalogRefreshEnabled must default false outside runDaemon()")
	}
	var on config.Config
	on.Mode.CatalogCheck = "on"
	maybeRefreshCatalog(on) // must not spawn a fetch
	if _, err := os.Stat(meta.CatalogCachePath()); err == nil {
		t.Error("maybeRefreshCatalog wrote a cache file while catalogRefreshEnabled was false")
	}
}

// TestCatalogCheckOffNeverFetchesEvenWhenEnabled: mode.catalog_check=off
// must short-circuit before the TTL/enabled check, so turning it off is a
// hard guarantee, not just a slower TTL.
func TestCatalogCheckOffNeverFetchesEvenWhenEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := catalogRefreshEnabled
	catalogRefreshEnabled = true
	defer func() { catalogRefreshEnabled = orig }()

	var off config.Config
	off.Mode.CatalogCheck = "off"
	maybeRefreshCatalog(off)
	if _, err := os.Stat(meta.CatalogCachePath()); err == nil {
		t.Error("maybeRefreshCatalog wrote a cache file with catalog_check=off")
	}
}
