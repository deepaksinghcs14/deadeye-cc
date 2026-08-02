package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
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
// failure to connect spawns the daemon detached for next time and returns
// {} immediately for this call -- a hook must never block a tool call on
// daemon startup.
func requestDaemon(event string, raw []byte) []byte {
	conn, err := net.DialTimeout("unix", meta.SocketPath(), 50*time.Millisecond)
	if err != nil {
		spawnDaemon()
		return []byte("{}")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))

	req := proto.Request{Event: event, Payload: raw, Off: config.OffSwitches()}
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

func spawnDaemon() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "daemon")
	detach(cmd)
	_ = cmd.Start()
}
