package meta

import (
	"os/exec"
	"strings"
	"testing"
)

// TestVersionIsLdflagsOverridable is the regression test for a real bug:
// Version was originally declared inside a `const (...)` block, which
// `go build -ldflags "-X pkg.Var=value"` silently cannot override (the -X
// flag only patches package-level string VARIABLES). Both the v0.1.0 and
// v0.2.0 releases shipped with this broken -- `deadeye version` reported
// the compiled-in dev string instead of the real release tag on every
// platform, caught only by running the actual downloaded release binary,
// not by any test. This builds a throwaway binary with the exact ldflags
// goreleaser uses and checks the override actually lands.
func TestVersionIsLdflagsOverridable(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	bin := t.TempDir() + "/metaverdtest"
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/deepaksinghcs14/deadeye-cc/internal/meta.Version=9.9.9-ldflags-test",
		"-o", bin,
		"./testdata/versionprobe",
	)
	build.Dir = "." // run from internal/meta
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("running probe binary: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "9.9.9-ldflags-test") {
		t.Errorf("ldflags -X override did not take effect -- got %q. "+
			"If Version was changed back to a const, this is why: "+
			"-ldflags -X cannot patch a const, only a var.", out)
	}
}
