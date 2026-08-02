package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// saveCapture writes the raw hook payload to ~/.deadeye/captures/ for the
// §10 verification sweep (docs/verified.md; PLAN.md §5 "Payload capture").
// Errors are swallowed: capturing is a debugging aid, never a reason to
// fail a hook.
func saveCapture(event string, raw []byte) {
	dir := meta.CapturesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	name := fmt.Sprintf("%s-%d.json", event, time.Now().UnixNano())
	_ = os.WriteFile(filepath.Join(dir, name), raw, 0o600)
}

// runCapture is a manual testing aid: `deadeye capture <event> < payload.json`
// saves stdin exactly like DEADEYE_CAPTURE=1 would, and echoes {}.
func runCapture(event string) {
	raw, _ := io.ReadAll(os.Stdin)
	saveCapture(event, raw)
	fmt.Print("{}")
}
