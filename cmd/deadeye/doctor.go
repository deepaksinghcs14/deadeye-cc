package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// `deadeye doctor` is the one command that answers "is this thing actually
// working", which nothing else did: `status` reports CONFIGURATION (what
// the modes are set to), and every failure mode deadeye has shipped is a
// failure of PLUMBING that leaves the configuration looking perfect.
//
// Every check below exists because the corresponding failure really
// happened and was invisible at the time: a judge that couldn't run at all
// because `claude` wasn't on PATH (fail-open, silent), a config.json that
// stopped parsing and reverted every setting to its default without
// saying so, a state dir created world-listable by one install path, a
// socket path too long for the platform's sockaddr limit, a daemon that
// wasn't running, a PATH binary shadowing a newer managed one, and a
// hooks manifest whose matcher didn't cover a tool the daemon dispatches
// (which silently retired a whole advisory for two releases, twice).
//
// It changes nothing. Read-only by contract, like `status` and `route`.

type checkResult struct {
	name   string
	status string // "ok" | "warn" | "fail"
	detail string
	fix    string // concrete next action, printed only when not ok
}

func runDoctor() {
	fmt.Printf("%s %s\n\n", cHead(meta.Name+" doctor"), cValue(meta.Version))

	checks := []checkResult{
		checkBinary(),
		checkStateDir(),
		checkConfigParses(),
		checkSocketPath(),
		checkDaemon(),
		checkJudge(),
		checkHooksManifest(),
		checkHosts(),
		checkStoreSizes(),
	}

	var warns, fails int
	for _, c := range checks {
		var mark string
		switch c.status {
		case "ok":
			mark = cGood("ok  ")
		case "warn":
			mark = cWarn("warn")
			warns++
		default:
			mark = cBad("FAIL")
			fails++
		}
		fmt.Printf("  %s  %-22s %s\n", mark, c.name, c.detail)
		if c.status != "ok" && c.fix != "" {
			fmt.Printf("        %s\n", cDim("→ "+c.fix))
		}
	}

	fmt.Println()
	switch {
	case fails > 0:
		fmt.Printf("%s %d failing, %d warning.\n", cBad("Not healthy:"), fails, warns)
		os.Exit(1)
	case warns > 0:
		fmt.Printf("%s %d warning.\n", cWarn("Working, with caveats:"), warns)
	default:
		fmt.Println(cGood("Healthy.") + cDim("  Modes and knobs: deadeye status"))
	}
}

// checkBinary: a `deadeye` on PATH older than the installed plugin shadows
// the managed one and silently serves stale behavior -- the skew that
// `status` warns about, repeated here so one command covers it.
func checkBinary() checkResult {
	self, err := os.Executable()
	if err != nil {
		self = "(unknown)"
	}
	managed := filepath.Join(meta.StateDir(), "bin", "deadeye")
	detail := fmt.Sprintf("running %s", self)

	pv := pluginVersion(os.Getenv("CLAUDE_PLUGIN_ROOT"))
	if pv != "" && semverNewer(pv, meta.Version) {
		return checkResult{"binary", "warn",
			detail + fmt.Sprintf(" (v%s, plugin has v%s)", meta.Version, pv),
			"a PATH binary is shadowing the managed one: run `which deadeye`, then remove it or run `deadeye update`"}
	}
	if _, err := os.Stat(managed); err != nil && self != managed {
		return checkResult{"binary", "warn", detail + " (no managed copy yet)",
			"normal before the first hook fires; it self-installs at the next SessionStart"}
	}
	return checkResult{"binary", "ok", detail, ""}
}

// checkStateDir: ~/.deadeye holds the decision log, which carries prompt
// markers and credential paths. One install path created it 0755.
func checkStateDir() checkResult {
	fi, err := os.Stat(meta.StateDir())
	if err != nil {
		return checkResult{"state dir", "warn", meta.StateDir() + " does not exist yet",
			"created on first use; nothing to do"}
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return checkResult{"state dir", "fail",
			fmt.Sprintf("%s is %04o -- readable by other users", meta.StateDir(), perm),
			"chmod 700 " + meta.StateDir() + "  (the decision log carries prompt text and paths)"}
	}
	return checkResult{"state dir", "ok", meta.StateDir() + " 0700", ""}
}

// checkConfigParses: config.Load fails OPEN to defaults by design, so a
// config.json that stopped parsing looks exactly like one that was never
// written -- every setting silently reverted, nothing said.
func checkConfigParses() checkResult {
	if err := config.ParseError(); err != nil {
		return checkResult{"config.json", "fail", "does not parse -- every setting is running at its default",
			"fix the JSON or delete it: " + meta.ConfigPath()}
	}
	if _, err := os.Stat(meta.ConfigPath()); err != nil {
		return checkResult{"config.json", "ok", "not present (all defaults)", ""}
	}
	return checkResult{"config.json", "ok", "parses", ""}
}

// checkSocketPath: a Unix sockaddr path caps near 104 bytes, so a long
// $HOME silently pushes the socket into TempDir. Worth seeing, because it
// changes where the daemon actually listens.
func checkSocketPath() checkResult {
	p := meta.SocketPath()
	if !strings.HasPrefix(p, meta.StateDir()) {
		return checkResult{"socket path", "warn",
			fmt.Sprintf("%s (fell back: %s is too long for a unix socket)", p, meta.StateDir()),
			"works as-is; the daemon and every client agree on this path"}
	}
	return checkResult{"socket path", "ok", p, ""}
}

// checkDaemon: measures a real round trip. The hook client gives up after
// ~200ms on most events, so a daemon that answers slower than that is
// advising nobody.
func checkDaemon() checkResult {
	start := time.Now()
	conn, err := net.DialTimeout("unix", meta.SocketPath(), 200*time.Millisecond)
	if err != nil {
		return checkResult{"daemon", "warn", "not running",
			"normal when idle -- it exits after 30 minutes and any hook respawns it"}
	}
	defer conn.Close()
	return checkResult{"daemon", "ok", fmt.Sprintf("listening (connect %v)", time.Since(start).Round(time.Millisecond)), ""}
}

// checkJudge: the AI routing judge is on by default and, since the Agent
// hook started waiting for it, produces most of routing's value. With no
// `claude` on PATH it fails open and never runs -- silently, forever.
func checkJudge() checkResult {
	cfg := config.Load()
	if cfg.Mode.RoutingJudge != "on" {
		return checkResult{"routing judge", "ok", "off (mode.routing_judge)", ""}
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return checkResult{"routing judge", "warn", "on, but `claude` is not on PATH -- it can never run",
			"put `claude` on PATH, or turn it off: deadeye config set mode.routing_judge off"}
	}
	return checkResult{"routing judge", "ok",
		fmt.Sprintf("on (a first-seen subtask waits up to %v for it)", judgeTimeout), ""}
}

// checkHooksManifest: a matcher that doesn't list a tool the daemon
// dispatches means that advisory never fires. It has shipped broken twice
// -- Grep in 0.9.0, Workflow from 0.58.0 to 0.61.1 -- because the unit
// tests call the handler directly and pass either way.
func checkHooksManifest() checkResult {
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		return checkResult{"hooks manifest", "ok", "not running under a plugin root (skipped)", ""}
	}
	path := filepath.Join(root, "hooks", "hooks.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return checkResult{"hooks manifest", "fail", "missing at " + path,
			"reinstall the plugin: /plugin"}
	}
	missing := toolsMissingFromMatchers(string(raw))
	if len(missing) > 0 {
		return checkResult{"hooks manifest", "fail",
			"no PreToolUse matcher covers: " + strings.Join(missing, ", "),
			"those advisories can never fire; reinstall the plugin or report it"}
	}
	return checkResult{"hooks manifest", "ok", "every dispatched tool is matched", ""}
}

// toolsMissingFromMatchers reports which tools decidePreToolUse handles
// that the manifest's PreToolUse matchers don't mention. Substring rather
// than a JSON walk: the question is only "does this tool name appear in a
// matcher", and a hook manifest is small.
func toolsMissingFromMatchers(manifest string) []string {
	var missing []string
	for _, tool := range preToolUseDispatchedTools {
		if !strings.Contains(manifest, tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

// preToolUseDispatchedTools is the Claude Code side of decidePreToolUse's
// switch. Kept next to a test that reads the real switch, so this list
// can't silently drift from the code it describes.
var preToolUseDispatchedTools = []string{"Bash", "Edit", "Write", "Agent", "Read", "Grep", "WebFetch", "Workflow"}

// checkHosts reports which non-Claude hosts have been initialized, so
// "deadeye isn't doing anything in Codex" has an answer.
func checkHosts() checkResult {
	var found []string
	if p, err := codexHooksPath(); err == nil {
		if _, err := os.Stat(p); err == nil {
			found = append(found, "codex")
		}
	}
	if _, err := os.Stat(geminiExtensionDir()); err == nil {
		found = append(found, "gemini")
	}
	cwd, _ := os.Getwd()
	for host, rel := range rulesFileRelPath {
		if cwd == "" {
			break
		}
		if _, err := os.Stat(filepath.Join(cwd, rel)); err == nil {
			found = append(found, host)
		}
	}
	if len(found) == 0 {
		return checkResult{"other hosts", "ok", "none initialized (Claude Code only)", ""}
	}
	return checkResult{"other hosts", "ok", strings.Join(found, ", "), ""}
}

// checkStoreSizes: both stores are read WHOLE on startup and on several
// commands, and rotate only past 10MB.
func checkStoreSizes() checkResult {
	var parts []string
	warn := false
	for _, s := range []struct {
		label string
		path  string
	}{{"decisions", meta.LogPath()}, {"outcomes", meta.OutcomesPath()}} {
		fi, err := os.Stat(s.path)
		if err != nil {
			parts = append(parts, s.label+" 0")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", s.label, humanBytes(fi.Size())))
		if fi.Size() > 8<<20 {
			warn = true
		}
	}
	rows, _ := logstore.Scan(meta.LogPath())
	detail := strings.Join(parts, ", ") + fmt.Sprintf(" (%d decisions)", len(rows))
	if warn {
		return checkResult{"stores", "warn", detail,
			"approaching the 10MB rotation point; `deadeye lessons reset` clears outcomes"}
	}
	return checkResult{"stores", "ok", detail, ""}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
