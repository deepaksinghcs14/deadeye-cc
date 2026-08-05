package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

const releaseLatest = "https://github.com/deepaksinghcs14/deadeye-cc/releases/latest/download"

// updateAssetName is the goreleaser artifact for this platform
// (formats: [binary], name_template "deadeye_{{.Os}}_{{.Arch}}").
func updateAssetName() string {
	name := "deadeye_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// binName is the locally-installed binary's filename -- deadeye-hook.ps1
// only ever probes "deadeye.exe" on Windows, so installing extensionless
// there leaves the hook still running whatever was there before while this
// command reports success and, on every later run, "already up to date"
// (comparing against a file it never actually updates).
func binName() string {
	if runtime.GOOS == "windows" {
		return meta.Name + ".exe"
	}
	return meta.Name
}

// wantedChecksum extracts the sha256 for asset from a checksums.txt body.
func wantedChecksum(checksums, asset string) string {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0]
		}
	}
	return ""
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// runUpdate backs `deadeye update`: one command to bring
// ~/.deadeye/bin/deadeye to the latest release. This is the manual-host
// (Codex) counterpart of the Claude-side plugin bootstrap -- hash-compare
// against the published checksums, download only on mismatch, sha256
// verify, atomic same-directory rename. The daemon version handshake
// retires the old daemon on the next hook call, so no restart is needed.
func runUpdate() {
	client := &http.Client{Timeout: 60 * time.Second}
	asset := updateAssetName()

	resp, err := client.Get(releaseLatest + "/checksums.txt")
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update: fetching checksums:", err)
		os.Exit(1)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "deadeye update: checksums.txt: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}
	want := wantedChecksum(string(body), asset)
	if want == "" {
		fmt.Fprintln(os.Stderr, "deadeye update: no", asset, "in the latest release's checksums.txt")
		os.Exit(1)
	}

	dest := filepath.Join(meta.StateDir(), "bin", binName())
	if sha256File(dest) == want {
		fmt.Println("Already up to date (" + cValue(meta.Version) + " at " + dest + ").")
		warnPathShadow(dest)
		return
	}

	resp, err = client.Get(releaseLatest + "/" + asset)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update: download:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "deadeye update: %s: HTTP %d\n", asset, resp.StatusCode)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update:", err)
		os.Exit(1)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".deadeye-update-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update:", err)
		os.Exit(1)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, io.LimitReader(resp.Body, 200<<20)); err != nil {
		tmp.Close()
		fmt.Fprintln(os.Stderr, "deadeye update: download:", err)
		os.Exit(1)
	}
	tmp.Close()

	if got := sha256File(tmpPath); got != want {
		fmt.Fprintln(os.Stderr, "deadeye update: sha256 mismatch -- refusing to install (got "+got[:12]+"..., want "+want[:12]+"...)")
		os.Exit(1)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update:", err)
		os.Exit(1)
	}
	// Same-directory rename: atomic, and safe to do over a running binary.
	if err := os.Rename(tmpPath, dest); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye update:", err)
		os.Exit(1)
	}

	// Report the version the NEW binary says it is -- measured, not assumed.
	newVersion := "installed"
	if out, err := exec.Command(dest, "version").Output(); err == nil {
		newVersion = strings.TrimSpace(string(out))
	}
	fmt.Println(cGood("Updated") + " " + dest + " -> " + cValue(newVersion))
	fmt.Println(cDim("The running daemon retires itself on the next hook call (version handshake) -- no restart needed."))
	warnPathShadow(dest)
}

// warnPathShadow flags a PATH binary that would take precedence over the
// copy this command manages -- hooks prefer PATH, so an old go-installed
// build would silently win over the fresh download.
func warnPathShadow(dest string) {
	if p, err := exec.LookPath("deadeye"); err == nil {
		if rp, _ := filepath.EvalSymlinks(p); rp != dest {
			if rd, _ := filepath.EvalSymlinks(dest); rd != rp {
				fmt.Println(cWarn("Note: " + p + " is on PATH and takes precedence over " + dest + " -- update that copy via the way you installed it (e.g. go install ...@latest)."))
			}
		}
	}
}
