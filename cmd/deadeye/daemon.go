package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/proto"
)

const idleTimeout = 30 * time.Minute

// runDaemon is the long-lived process behind the unix socket. Only one
// instance should ever be listening; startup is singleflight via an
// O_EXCL lockfile so concurrent hook invocations racing to start it can't
// both succeed (PLAN.md §5.3 note in the amended §5: "daemon + unix
// socket client" -- INV-8's latency budget is met without it, per
// docs/verified.md V8, but it's built per the approved Phase 0 scope).
func runDaemon() {
	if err := os.MkdirAll(meta.StateDir(), 0o700); err != nil {
		return
	}

	lock, err := acquireLock(meta.LockPath())
	if err != nil {
		return // another daemon is running or starting; let it own the socket
	}
	defer func() {
		lock.Close()
		os.Remove(meta.LockPath())
	}()

	sockPath := meta.SocketPath()
	if probeAlive(sockPath) {
		return // a listener is already answering; nothing to do
	}
	os.Remove(sockPath) // clear a stale socket file from a crashed daemon

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return
	}
	defer l.Close()
	_ = os.Chmod(sockPath, 0o600)

	state := newDaemonState(catalog.Load(), logstore.Open(meta.LogPath()))

	connCh := make(chan net.Conn)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				close(connCh)
				return
			}
			connCh <- conn
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	for {
		select {
		case conn, ok := <-connCh:
			if !ok {
				return
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleTimeout)
			go handleConn(conn, state)
		case <-idle.C:
			return
		case <-sigCh:
			return
		}
	}
}

// handleConn reads one request to EOF (the client half-closes after
// writing), decides, and writes the raw hookio.Output JSON back.
func handleConn(conn net.Conn, state *daemonState) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	raw, err := io.ReadAll(conn)
	if err != nil {
		conn.Write([]byte("{}"))
		return
	}

	var req proto.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		conn.Write([]byte("{}"))
		return
	}

	out := decide(req, state)
	b, err := json.Marshal(out)
	if err != nil {
		b = []byte("{}")
	}
	conn.Write(b)
}

func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 60*time.Second {
			_ = os.Remove(path)
			return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		}
		return nil, err
	}
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()))
	return f, nil
}

func probeAlive(sockPath string) bool {
	conn, err := net.DialTimeout("unix", sockPath, 50*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
