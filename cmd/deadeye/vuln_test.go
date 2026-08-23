package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/secscan"
)

// noNetworkOSV replaces triggerOSVRefresh for the duration of the test so
// nothing here makes a real outbound request; restores the original after.
func noNetworkOSV(t *testing.T) {
	t.Helper()
	orig := triggerOSVRefresh
	triggerOSVRefresh = func(deps []secscan.Dep) {}
	t.Cleanup(func() { triggerOSVRefresh = orig })
}

func editIn(sessionID, filePath, newString string) hookio.Input {
	b, _ := json.Marshal(map[string]string{"file_path": filePath, "new_string": newString})
	return hookio.Input{SessionID: sessionID, ToolName: "Edit", ToolInput: b}
}

func TestDecideVulnAdviceFiresOnSQLConcat(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	in := editIn("v1", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	out := decideVulnAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "deadeye:") {
		t.Fatalf("expected a vuln advisory, got %+v", out.HookSpecificOutput)
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "bind") {
		t.Errorf("advisory should name the fix, got %q", out.HookSpecificOutput.AdditionalContext)
	}
}

func TestDecideVulnAdviceDedupesWithinSession(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	in := editIn("v2", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	first := decideVulnAdvice(in, config.Default(), state)
	if first.HookSpecificOutput == nil {
		t.Fatal("first edit should be advised")
	}
	second := decideVulnAdvice(in, config.Default(), state)
	if second.HookSpecificOutput != nil {
		t.Errorf("identical second edit should be silent (dedupe), got %+v", second.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceCleanEditIsSilent(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	in := editIn("v3", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name=?", name)`)

	if out := decideVulnAdvice(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("parameterized query should not be flagged, got %+v", out.HookSpecificOutput)
	}
}

// TestDecideVulnAdviceSurvivesPersonaOff pins the 0.17.0 decoupling:
// turning the coder persona LEVEL off ("stop coder" / /deadeye-coder off,
// stored as session level "off") must NOT silence the security advisory --
// only coder.security:"off" and the env kill switches (Coder.Disabled) do.
func TestDecideVulnAdviceSurvivesPersonaOff(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	state.setCoderLevel("v0", "off") // the persona is off for this session
	in := editIn("v0", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	out := decideVulnAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "deadeye:") {
		t.Fatalf("security advisory must survive persona-off, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceRespectsCoderDisabled(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	cfg := config.Default()
	cfg.Coder.Disabled = true
	in := editIn("v4", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	if out := decideVulnAdvice(in, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("coder disabled but still advised: %+v", out.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceRespectsSecurityOff(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	cfg := config.Default()
	cfg.Coder.Security = "off"
	in := editIn("v5", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	if out := decideVulnAdvice(in, cfg, state); out.HookSpecificOutput != nil {
		t.Errorf("coder.security off but still advised: %+v", out.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceRespectsMute(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	state.setMuted("v6", true)
	in := editIn("v6", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	if out := decideVulnAdvice(in, config.Default(), state); out.HookSpecificOutput != nil {
		t.Errorf("muted session but still advised: %+v", out.HookSpecificOutput)
	}
}

// TestDecideVulnAdviceManifestSupersededFallback: with an isolated HOME
// (coderTestState), the OSV cache file doesn't exist -- ScanDeps must fall
// back to the bundled superseded table rather than staying silent, and
// must not need triggerOSVRefresh's real implementation to do it (see
// noNetworkOSV).
func TestDecideVulnAdviceManifestSupersededFallback(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	in := editIn("v7", "package.json", `    "moment": "2.29.0",`)

	out := decideVulnAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "moment") {
		t.Fatalf("expected a superseded-dep advisory naming moment, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceSecurityOSVOffStaysOffline(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	cfg := config.Default()
	off := false
	cfg.Coder.SecurityOSV = &off
	in := editIn("v8", "package.json", `    "moment": "2.29.0",`)

	// The bundled table must still fire even with OSV lookups disabled --
	// "degrades, doesn't vanish" per the plan.
	out := decideVulnAdvice(in, cfg, state)
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "moment") {
		t.Fatalf("bundled table should still work with security_osv:false, got %+v", out.HookSpecificOutput)
	}
}

func TestDecideVulnAdviceCapsAtTwoFindings(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	// Three distinct rules in one edit: secret literal, weak crypto near a
	// password, and TLS off.
	body := "password := \"hunter2fake9\"\n" +
		"hash := md5.Sum([]byte(password))\n" +
		"tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}"
	in := editIn("v9", "handlers.go", body)

	out := decideVulnAdvice(in, config.Default(), state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected an advisory")
	}
	got := strings.Count(out.HookSpecificOutput.AdditionalContext, "deadeye:")
	if got != vulnFindingCap {
		t.Errorf("got %d advisories, want the cap of %d", got, vulnFindingCap)
	}
}

func TestParseApplyPatch(t *testing.T) {
	patch := "*** Begin Patch\n" +
		"*** Update File: handlers.go\n" +
		"@@\n" +
		"-// TODO\n" +
		"+rows, err := db.Query(\"SELECT * FROM users WHERE name='\" + name + \"'\")\n" +
		"*** End Patch\n"
	targets := parseApplyPatch(patch)
	if len(targets) != 1 || targets[0].Path != "handlers.go" {
		t.Fatalf("got %+v, want one target for handlers.go", targets)
	}
	if !strings.Contains(targets[0].Added, "SELECT") {
		t.Errorf("added text missing the '+' line content: %q", targets[0].Added)
	}
}

func TestParseApplyPatchSkipsDeletedFiles(t *testing.T) {
	patch := "*** Begin Patch\n*** Delete File: old.go\n*** End Patch\n"
	if targets := parseApplyPatch(patch); len(targets) != 0 {
		t.Errorf("deleted file should carry no targets, got %+v", targets)
	}
}

func TestExtractEditTargetsApplyPatch(t *testing.T) {
	// key literal split across a concatenation so it doesn't read as a
	// real credential to secret scanners on the committed diff.
	fakeKey := "AKIA" + "ABCDEFGHIJKLMNOP"
	b, _ := json.Marshal(map[string]string{
		"command": "*** Begin Patch\n*** Add File: config.go\n+key := \"" + fakeKey + "\"\n*** End Patch\n",
	})
	targets := extractEditTargets("apply_patch", b)
	if len(targets) != 1 || targets[0].Path != "config.go" {
		t.Fatalf("got %+v", targets)
	}
}

// TestDecidePreToolUseMergesGateAndVuln: the plan gate's PermissionDecision
// and the vuln advisory's AdditionalContext are different fields of the
// same hookSpecificOutput -- both must survive when a single Edit trips
// both surfaces.
func TestDecidePreToolUseMergesGateAndVuln(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	cfg := config.Default()
	cfg.Mode.PlanGate = "hard"
	state.setPendingPlan("v10", "add a feature")
	in := editIn("v10", "handlers.go", `rows, err := db.Query("SELECT * FROM users WHERE name='" + name + "'")`)

	out := decidePreToolUse(in, cfg, state)
	if out.HookSpecificOutput == nil {
		t.Fatal("expected a combined response")
	}
	if out.HookSpecificOutput.PermissionDecision != hookio.PermissionAsk {
		t.Errorf("gate's permission-ask was dropped: %+v", out.HookSpecificOutput)
	}
	if !strings.Contains(out.HookSpecificOutput.AdditionalContext, "deadeye:") {
		t.Errorf("vuln advisory was dropped: %+v", out.HookSpecificOutput)
	}
}

// TestVulnFixturesRespondWellUnderTimeout: the hook has a 5s timeout and
// no cache should exist under an isolated HOME -- decidePreToolUse must
// return fast even for a manifest edit that would trigger an OSV refresh
// (the goroutine it spawns must never be on this path; noNetworkOSV
// replaces it, but the timing bound documents the real contract: the
// production triggerOSVRefresh call itself is also non-blocking).
func TestVulnFixturesRespondWellUnderTimeout(t *testing.T) {
	noNetworkOSV(t)
	state := coderTestState(t)
	cfg := config.Default()

	for _, fixture := range []string{"../../testdata/payloads/pretooluse_edit.json", "../../testdata/payloads/pretooluse_edit_manifest.json"} {
		b, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		var in hookio.Input
		if err := json.Unmarshal(b, &in); err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		start := time.Now()
		out := decidePreToolUse(in, cfg, state)
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("%s: took %v, want well under the 5s hook timeout", fixture, elapsed)
		}
		if out.HookSpecificOutput == nil {
			t.Errorf("%s: expected an advisory", fixture)
		}
	}
}
