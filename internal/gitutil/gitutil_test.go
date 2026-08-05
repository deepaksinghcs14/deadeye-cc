package gitutil

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProjectKeySanitizes(t *testing.T) {
	got := ProjectKey("/tmp/does-not-exist/my project!@#")
	if strings.ContainsAny(got, " !@#") {
		t.Errorf("ProjectKey did not sanitize: %q", got)
	}
}

func TestOutputFailsOpenOutsideGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	if got := Output(t.TempDir(), "rev-parse", "--show-toplevel"); got != "" {
		t.Errorf("expected \"\" outside a git repo, got %q", got)
	}
}

func TestOutputCtxRespectsExpiredDeadline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	if got := OutputCtx(ctx, ".", "version"); got != "" {
		t.Errorf("expected \"\" under an expired deadline, got %q", got)
	}
}
