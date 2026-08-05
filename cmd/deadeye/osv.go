package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/secscan"
)

// osvBatchURL is OSV.dev's free, keyless batch-query endpoint -- same
// unauthenticated stdlib-net/http approach as `deadeye update`'s release
// check (cmd/deadeye/update.go), no third-party client library.
const osvBatchURL = "https://api.osv.dev/v1/querybatch"

// osvInFlight dedupes concurrent refreshes for the same dependency key --
// several quick edits touching the same still-unknown dep must not each
// spawn their own OSV request.
var osvInFlight = struct {
	mu   sync.Mutex
	keys map[string]bool
}{keys: map[string]bool{}}

func osvCacheKey(d secscan.Dep) string { return d.Ecosystem + ":" + d.Name + "@" + d.Version }

// triggerOSVRefresh spawns a background goroutine that queries OSV.dev for
// deps and merges the result into ~/.deadeye/osv-cache.json. This
// function itself never blocks, and the goroutine it starts must never sit
// on any hook's response path -- INV-5's fail-open extends to latency
// here: a hook has a 5s timeout and the network is never guaranteed to be
// reachable. The CURRENT edit gets whatever the cache already knew; a
// refresh only informs the NEXT one.
//
// A package-level var, not a plain func, so tests can swap in a no-op --
// same seam as color.go's colorOn -- and keep the test suite from making
// real outbound requests.
var triggerOSVRefresh = func(deps []secscan.Dep) {
	osvInFlight.mu.Lock()
	var fresh []secscan.Dep
	for _, d := range deps {
		k := osvCacheKey(d)
		if osvInFlight.keys[k] {
			continue
		}
		osvInFlight.keys[k] = true
		fresh = append(fresh, d)
	}
	osvInFlight.mu.Unlock()
	if len(fresh) == 0 {
		return
	}

	go func() {
		defer func() {
			osvInFlight.mu.Lock()
			for _, d := range fresh {
				delete(osvInFlight.keys, osvCacheKey(d))
			}
			osvInFlight.mu.Unlock()
		}()
		refreshOSVCache(fresh)
	}()
}

type osvQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version"`
}

type osvVuln struct {
	ID string `json:"id"`
}

type osvResult struct {
	Vulns []osvVuln `json:"vulns"`
}

type osvBatchResponse struct {
	Results []osvResult `json:"results"`
}

// refreshOSVCache queries OSV.dev for exactly deps -- package name and
// version only, nothing else about the codebase (see README's
// disclosure). A short client timeout and "give up silently on any error"
// keep this best-effort, matching every other advisory surface's
// fail-open rule: a network hiccup here degrades to "cache stays as it
// was," never an error surfaced anywhere.
func refreshOSVCache(deps []secscan.Dep) {
	var reqBody struct {
		Queries []osvQuery `json:"queries"`
	}
	for _, d := range deps {
		var q osvQuery
		q.Package.Name = d.Name
		q.Package.Ecosystem = d.Ecosystem
		q.Version = d.Version
		reqBody.Queries = append(reqBody.Queries, q)
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Post(osvBatchURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return
	}
	var out osvBatchResponse
	if json.Unmarshal(body, &out) != nil || len(out.Results) != len(deps) {
		return // shape mismatch -- don't guess which result maps to which dep
	}

	// querybatch returns only vuln IDs, not fix versions -- a second
	// per-ID lookup would get the exact patched version, but that doubles
	// network cost for a background best-effort cache. The advisory names
	// the ID and points at checking for a patch; /deadeye-guard's native
	// auditors are where the precise fixed-version answer lives.
	entries := map[string][]string{}
	for i, d := range deps {
		key := osvCacheKey(d)
		if len(out.Results[i].Vulns) == 0 {
			entries[key] = []string{} // known-clean: stops re-querying within the TTL
			continue
		}
		var ids []string
		for _, v := range out.Results[i].Vulns {
			ids = append(ids, v.ID)
			if len(ids) == 2 {
				break
			}
		}
		entries[key] = []string{fmt.Sprintf("%s %s has an open OSV advisory (%s) -- check for a patched version.", d.Name, d.Version, strings.Join(ids, ", "))}
	}
	mergeOSVCache(entries)
}

var osvCacheWriteMu sync.Mutex

// mergeOSVCache folds entries into ~/.deadeye/osv-cache.json's existing
// contents and bumps the whole file's fetched time.
//
// deadeye: freshness is tracked per FILE, not per entry -- merging in one
// dep's fresh result marks every other cached entry fresh too, even ones
// untouched this round. ceiling: an entry can read up to ~24h staler than
// its neighbor without anything re-checking it early. upgrade: per-entry
// timestamps, if a real staleness complaint shows up.
func mergeOSVCache(newEntries map[string][]string) {
	osvCacheWriteMu.Lock()
	defer osvCacheWriteMu.Unlock()

	path := meta.OSVCachePath()
	now := time.Now().Unix()
	existing := secscan.LoadOSVCache(path, now)
	merged := existing.Entries
	if merged == nil {
		merged = map[string][]string{}
	}
	for k, v := range newEntries {
		merged[k] = v
	}
	b, err := json.Marshal(struct {
		FetchedUnix int64               `json:"fetched_unix"`
		Entries     map[string][]string `json:"entries"`
	}{FetchedUnix: now, Entries: merged})
	if err != nil {
		return
	}
	_ = os.MkdirAll(meta.StateDir(), 0o700)
	_ = os.WriteFile(path, b, 0o600) // best-effort; a failed write just means the next edit re-triggers (INV-5)
}
