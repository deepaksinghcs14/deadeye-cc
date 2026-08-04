package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/proto"
)

// dialDaemon is a seam over requestDaemon so tests can force failures
// (including panics) without a real socket.
var dialDaemon = requestDaemon

// runHook is the PreToolUse/SessionStart/etc entry point invoked by
// hooks/deadeye-hook.sh once per tool call.
func runHook(event string) {
	runHookTo(os.Stdout, os.Stdin, event)
}

// runHookTo is runHook with injected I/O, so tests can drive it without
// touching the real stdin/stdout. It is wrapped in a top-level recover so
// that any panic anywhere in this call graph still prints the canonical
// "{}" -- INV-5 (fail open) is structural here, not incidental, because
// this sits in the critical path of every matched tool call (INV-8).
func runHookTo(w io.Writer, r io.Reader, event string) {
	defer func() {
		if rec := recover(); rec != nil {
			// Fail open (INV-5), but leave a trace -- a swallowed panic
			// with no trail makes "deadeye stopped advising" undiagnosable.
			logPanic("client/"+event, rec)
			fmt.Fprint(w, "{}")
		}
	}()

	raw, _ := io.ReadAll(r)

	if os.Getenv("DEADEYE_CAPTURE") == "1" {
		saveCapture(event, raw)
	}

	off := config.OffSwitches()
	for _, v := range off {
		if v == "DEADEYE" {
			fmt.Fprint(w, "{}")
			return
		}
	}

	out := dialDaemon(event, raw)
	if len(out) == 0 {
		out = []byte("{}")
	}
	w.Write(out)
}

// requestDaemon dials the daemon with a hard 200ms deadline (INV-8's 50ms
// p95 budget with headroom for the occasional cold daemon start). Any
// failure to connect spawns the daemon detached and, for the hot-path
// events, returns {} immediately -- a hook must never block a tool call
// on daemon startup.
//
// SessionStart is the one exception: it fires once per session, its hook
// timeout is 5s, and it carries the coder-persona injection -- failing
// open there means the WHOLE session runs without the persona (caught
// live: the first real session after a daemon exit answered "Unknown" to
// its own coder level; the very next session answered correctly). So for
// SessionStart only, wait briefly for the daemon just spawned and retry.
func requestDaemon(event string, raw []byte) []byte {
	conn, err := net.DialTimeout("unix", meta.SocketPath(), 50*time.Millisecond)
	if err != nil {
		spawnDaemonFn()
		if event != "SessionStart" {
			return []byte("{}")
		}
		conn = awaitDaemon(2 * time.Second)
		if conn == nil {
			return []byte("{}")
		}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))

	req := proto.Request{
		Event: event, Payload: raw, Off: config.OffSwitches(),
		PluginRoot:    os.Getenv("CLAUDE_PLUGIN_ROOT"),
		ConfigDir:     os.Getenv("CLAUDE_CONFIG_DIR"),
		ClientVersion: clientVersion(),
		PluginVersion: meta.Version,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return []byte("{}")
	}
	if _, err := conn.Write(b); err != nil {
		return []byte("{}")
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		return []byte("{}")
	}
	return resp
}

// spawnDaemonFn is a seam so tests can stub the spawn -- calling the real
// one from a test binary would detach a copy of the TEST binary with a
// "daemon" arg it doesn't understand.
var spawnDaemonFn = spawnDaemon

func spawnDaemon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "daemon")
	detach(cmd)
	_ = cmd.Start()
}

// awaitDaemon polls for the daemon socket until it answers or the budget
// runs out. SessionStart-only -- see requestDaemon.
func awaitDaemon(budget time.Duration) net.Conn {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if conn, err := net.DialTimeout("unix", meta.SocketPath(), 50*time.Millisecond); err == nil {
			return conn
		}
	}
	return nil
}

// clientVersion parses the Claude Code version from CLAUDE_CODE_EXECPATH's
// basename (.../versions/2.1.220 -- verified live in a real hook env).
// "" when the layout is unrecognized; the daemon fails open on "".
func clientVersion() string {
	base := filepath.Base(os.Getenv("CLAUDE_CODE_EXECPATH"))
	for i, r := range base {
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' && i > 0 {
			continue
		}
		return ""
	}
	if base == "" || base == "." {
		return ""
	}
	return base
}

// logPanic leaves a one-line trace in ~/.deadeye/panics.log. Best-effort
// by design: the panic path must never itself become a failure mode.
func logPanic(where string, rec any) {
	f, err := os.OpenFile(filepath.Join(meta.StateDir(), "panics.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%s %s: %v\n", time.Now().UTC().Format(time.RFC3339), where, rec)
	f.Close()
}
