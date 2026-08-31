package main

import (
	"strings"
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
)

func TestSemverNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.26.0", "0.25.0", true},
		{"0.25.1", "0.25.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.25.0", "0.25.0", false},
		{"0.24.0", "0.25.0", false},
		{"v0.26.0", "0.25.0-dev", true}, // v prefix + -dev suffix both tolerated
		{"0.25.0", "0.25.0-dev", false}, // same base -> not newer, don't ask
		{"garbage", "0.25.0", false},    // malformed -> never ask
		{"0.26", "0.25.0", false},       // wrong arity -> never ask
	}
	for _, c := range cases {
		if got := semverNewer(c.a, c.b); got != c.want {
			t.Errorf("semverNewer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestUpdateNudge: with a cached newer version, the ask fires once, then goes
// quiet for that version; update_check=off silences it entirely. A fresh
// CheckedUnix keeps the background refresh (network) from firing in the test.
func TestUpdateNudge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st := newDaemonState(catalog.Catalog{}, nil)

	saveUpdateCache(updateCache{CheckedUnix: nowUnix(), Latest: "99.0.0"})
	var on config.Config
	on.Mode.UpdateCheck = "on"

	u := updateNudge(on, st, "s1")
	if u == "" || !strings.Contains(u, "99.0.0") {
		t.Errorf("expected update ask mentioning 99.0.0, got %q", u)
	}
	if u := updateNudge(on, st, "s1"); u != "" {
		t.Error("must ask at most once per version")
	}

	// a still-newer version + off -> silent
	saveUpdateCache(updateCache{CheckedUnix: nowUnix(), Latest: "100.0.0"})
	var off config.Config
	off.Mode.UpdateCheck = "off"
	if u := updateNudge(off, st, "s1"); u != "" {
		t.Error("update_check=off must silence the ask")
	}
}
