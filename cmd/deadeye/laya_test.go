package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/hookio"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// layaServer stands in for laya-serve, always answering with the given
// body.
func layaServer(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func layaCfg(mode, endpoint string) config.Config {
	cfg := config.Default()
	cfg.Mode.Laya = mode
	cfg.Laya.Endpoint = endpoint
	cfg.Laya.APIKeyEnv = ""
	cfg.Laya.TimeoutMS = 2000
	return cfg
}

// The default must be off, so an upgrade changes nothing for anyone who
// hasn't opted in.
func TestLayaDefaultsOff(t *testing.T) {
	cfg := config.Default()
	if cfg.Mode.Laya != layaOff {
		t.Errorf("mode.laya default = %q, want off", cfg.Mode.Laya)
	}
	if cfg.Laya.Endpoint != "" {
		t.Errorf("laya.endpoint default = %q, want empty", cfg.Laya.Endpoint)
	}
	if layaEnabled(cfg) || layaDecides(cfg) {
		t.Error("Laya must be neither enabled nor authoritative by default")
	}
	if layaClient(cfg) != nil {
		t.Error("layaClient must be nil when off")
	}
}

// The ladder's whole point: only the top rung may change behavior, and no
// rung works without an endpoint.
func TestLayaLadderGating(t *testing.T) {
	cases := []struct {
		mode        string
		endpoint    string
		wantEnabled bool
		wantDecides bool
	}{
		{layaOff, "http://x", false, false},
		{layaShadow, "http://x", true, false},
		{layaAdvise, "http://x", true, false},
		{layaAuthoritative, "http://x", true, true},
		{layaShadow, "", false, false},
		{layaAuthoritative, "", false, false},
		{"nonsense", "http://x", false, false},
	}
	for _, c := range cases {
		cfg := layaCfg(c.mode, c.endpoint)
		if got := layaEnabled(cfg); got != c.wantEnabled {
			t.Errorf("mode=%q endpoint=%q: enabled=%v want %v", c.mode, c.endpoint, got, c.wantEnabled)
		}
		if got := layaDecides(cfg); got != c.wantDecides {
			t.Errorf("mode=%q endpoint=%q: decides=%v want %v", c.mode, c.endpoint, got, c.wantDecides)
		}
	}
}

func TestLayaTierParsesChoice(t *testing.T) {
	c := laya.New(layaServer(t, `{"answers":{"q":{"choice":"2","answer_confidence":0.77}}}`), "", time.Second)
	tier, certainty, ok := layaTier(context.Background(), c, "rewrite the routing kernel")
	if !ok || tier != 2 || certainty != 0.77 {
		t.Errorf("layaTier = %d,%v,%v; want 2,0.77,true", tier, certainty, ok)
	}
}

// A label outside the 0/1/2 vocabulary is rejected rather than coerced --
// silently mapping an unknown answer onto a tier would be inventing a
// routing decision.
func TestLayaTierRejectsUnknownLabel(t *testing.T) {
	for _, body := range []string{
		`{"answers":{"q":{"choice":"9"}}}`,
		`{"answers":{"q":{"choice":"tier two"}}}`,
		`{"answers":{"q":{"choice":""}}}`,
	} {
		c := laya.New(layaServer(t, body), "", time.Second)
		if tier, _, ok := layaTier(context.Background(), c, "x"); ok || tier != -1 {
			t.Errorf("body %s: got tier %d ok=%v; want -1,false", body, tier, ok)
		}
	}
}

// Shadow records the verdict and leaves the decision untouched, including
// the reason string -- an unchanged decision must be unchanged in full.
func TestLayaRoutingShadowChangesNothing(t *testing.T) {
	st, outPath := sampleHarness(t, 0, true)
	cfg := layaCfg(layaShadow, layaServer(t, `{"answers":{"q":{"choice":"0","answer_confidence":0.9}}}`))
	before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "original reason", Unsure: true}

	after, tier, ok := st.layaRouting(cfg, before, "some task")
	if !ok || tier != 0 {
		t.Fatalf("layaRouting = tier %d ok=%v; want 0,true", tier, ok)
	}
	if after != before {
		t.Errorf("shadow changed the decision:\n got %+v\nwant %+v", after, before)
	}
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("shadow wrote %d tier-disagreement rows; it should write none itself", n)
	}
}

// Advise annotates the visible reason but still changes no behavior.
func TestLayaRoutingAdviseAnnotatesOnly(t *testing.T) {
	st, _ := sampleHarness(t, 0, true)
	cfg := layaCfg(layaAdvise, layaServer(t, `{"answers":{"q":{"choice":"1","answer_confidence":0.8}}}`))
	before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "original", Unsure: true}

	after, _, _ := st.layaRouting(cfg, before, "task")
	if after.Model != before.Model || after.Effort != before.Effort || after.Unsure != before.Unsure {
		t.Errorf("advise changed behavior: %+v", after)
	}
	if !strings.Contains(after.Reason, "laya: tier 1") || !strings.Contains(after.Reason, "not applied") {
		t.Errorf("advise should annotate the reason and say it wasn't applied, got %q", after.Reason)
	}
}

// Authoritative replaces the judge for exactly the case the judge exists
// for: an Unsure decision. That is the cost argument for the whole feature.
func TestLayaRoutingAuthoritativeResolvesUnsure(t *testing.T) {
	st, _ := sampleHarness(t, 0, true)
	cfg := layaCfg(layaAuthoritative, layaServer(t, `{"answers":{"q":{"choice":"0","answer_confidence":0.9}}}`))
	before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "thin evidence", Unsure: true}

	after, _, _ := st.layaRouting(cfg, before, "rename a variable")
	if after.Unsure {
		t.Error("authoritative should resolve Unsure so the judge returns early")
	}
	if after.Model == before.Model {
		t.Error("authoritative should have re-resolved the model from Laya's tier")
	}
	if !strings.Contains(after.Reason, "laya classified") {
		t.Errorf("reason should name Laya as the decider, got %q", after.Reason)
	}
}

// A CONFIDENT decision is not the judge's business and not Laya's either:
// authoritative must not re-route something the heuristics already settled.
func TestLayaRoutingAuthoritativeLeavesConfidentDecisionAlone(t *testing.T) {
	st, _ := sampleHarness(t, 0, true)
	cfg := layaCfg(layaAuthoritative, layaServer(t, `{"answers":{"q":{"choice":"0"}}}`))
	before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "strong evidence", Confidence: 1, Unsure: false}

	after, _, ok := st.layaRouting(cfg, before, "task")
	if !ok {
		t.Fatal("expected Laya to answer")
	}
	if after != before {
		t.Errorf("a confident decision was modified:\n got %+v\nwant %+v", after, before)
	}
}

// Unreachable, unsure, or off: the decision must come back byte-identical.
func TestLayaRoutingFailsOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  config.Config
	}{
		{"off", layaCfg(layaOff, "http://127.0.0.1:1")},
		{"unreachable", layaCfg(layaAuthoritative, "http://127.0.0.1:1")},
		{"garbage answer", layaCfg(layaAuthoritative, layaServer(t, `{"answers":{"q":{"choice":"nope"}}}`))},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, _ := sampleHarness(t, 0, true)
			before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "r", Unsure: true}
			after, _, ok := st.layaRouting(c.cfg, before, "task")
			if ok {
				t.Error("expected no usable answer")
			}
			if after != before {
				t.Errorf("decision changed on failure:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

// A gate the heuristic fired survives unless Laya is authoritative AND
// confidently says no. Every other combination keeps it.
func TestLayaConfirmsGate(t *testing.T) {
	cases := []struct {
		name string
		mode string
		body string
		keep bool
	}{
		{"off keeps", layaOff, `{"answers":{"q":{"noul":0.01}}}`, true},
		{"shadow keeps despite low p", layaShadow, `{"answers":{"q":{"noul":0.01}}}`, true},
		{"advise keeps despite low p", layaAdvise, `{"answers":{"q":{"noul":0.01}}}`, true},
		{"authoritative suppresses on low p", layaAuthoritative, `{"answers":{"q":{"noul":0.01}}}`, false},
		{"authoritative keeps on high p", layaAuthoritative, `{"answers":{"q":{"noul":0.9}}}`, true},
		{"unreachable keeps", layaAuthoritative, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _ := sampleHarness(t, 0, true)
			endpoint := "http://127.0.0.1:1"
			if c.body != "" {
				endpoint = layaServer(t, c.body)
			}
			cfg := layaCfg(c.mode, endpoint)
			got := st.layaConfirmsGate(cfg, sitePlanGate, "do a big refactor", "Needs a plan?", "marker", "sess", t.TempDir())
			if got != c.keep {
				t.Errorf("keep = %v, want %v", got, c.keep)
			}
		})
	}
}

func TestLayaAgreementReport(t *testing.T) {
	now := time.Now()
	ts := now.Add(-time.Hour).Format(time.RFC3339)
	op := writeOutcomes(t,
		lessons.Outcome{TS: ts, Kind: KindLayaVerdict, Site: siteJudge, LayaValue: "1", Actual: "1"},
		lessons.Outcome{TS: ts, Kind: KindLayaVerdict, Site: siteJudge, LayaValue: "0", Actual: "2"},
		lessons.Outcome{TS: ts, Kind: KindLayaVerdict, Site: sitePlanGate, LayaValue: "true", Actual: "true"},
		// Stale: outside the 30-day window.
		lessons.Outcome{TS: now.Add(-60 * 24 * time.Hour).Format(time.RFC3339), Kind: KindLayaVerdict, Site: siteJudge, LayaValue: "0", Actual: "0"},
		// Another kind entirely: must not be counted.
		lessons.Outcome{TS: ts, Kind: "escalation", TaskShape: "code-edit"},
	)
	out := captureStdout(t, func() { layaAgreement(op, layaCfg(layaShadow, "http://x"), now) })

	if !strings.Contains(out, "3 verdicts") {
		t.Errorf("want 3 in-window verdicts counted:\n%s", out)
	}
	if !strings.Contains(out, siteJudge) || !strings.Contains(out, sitePlanGate) {
		t.Errorf("want a per-site breakdown:\n%s", out)
	}
	// The load-bearing caveat: agreement is not accuracy.
	for _, want := range []string{"AGREEMENT, not accuracy", "near chance", "no", "independent evaluation"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing caveat %q:\n%s", want, out)
		}
	}
}

func TestLayaAgreementEmptyStatePointsAtSetup(t *testing.T) {
	out := captureStdout(t, func() { layaAgreement(writeOutcomes(t), config.Default(), time.Now()) })
	if !strings.Contains(out, "No Laya verdicts") || !strings.Contains(out, "/deadeye-laya") {
		t.Errorf("empty state should point at the setup skill:\n%s", out)
	}
}

// Every documented way to turn Laya off must actually work. These are the
// instructions the /deadeye-laya skill, the README and the site page all
// give, so a broken one is a broken promise.
func TestEveryDocumentedOffSwitchWorks(t *testing.T) {
	t.Run("mode.laya off", func(t *testing.T) {
		cfg := layaCfg(layaOff, "http://127.0.0.1:8000")
		if layaEnabled(cfg) || layaDecides(cfg) || layaClient(cfg) != nil {
			t.Error("mode.laya=off did not disable Laya")
		}
	})
	t.Run("clearing the endpoint", func(t *testing.T) {
		cfg := layaCfg(layaAuthoritative, "")
		if layaEnabled(cfg) || layaDecides(cfg) || layaClient(cfg) != nil {
			t.Error("an empty endpoint did not disable Laya")
		}
	})
	t.Run("DEADEYE_LAYA=off", func(t *testing.T) {
		t.Setenv("DEADEYE_LAYA", "off")
		off := config.OffSwitches()
		found := false
		for _, v := range off {
			if v == "DEADEYE_LAYA" {
				found = true
			}
		}
		if !found {
			t.Fatal("DEADEYE_LAYA is not a recognized kill switch")
		}
		cfg := config.LoadFor("", off)
		if cfg.Mode.Laya != layaOff {
			t.Errorf("mode.laya = %q under DEADEYE_LAYA=off, want off", cfg.Mode.Laya)
		}
	})
	t.Run("DEADEYE=off covers it too", func(t *testing.T) {
		t.Setenv("DEADEYE", "off")
		cfg := config.LoadFor("", config.OffSwitches())
		if cfg.Mode.Laya != layaOff || cfg.Mode.TierSample != "off" {
			t.Errorf("total off left laya=%q tier_sample=%q", cfg.Mode.Laya, cfg.Mode.TierSample)
		}
	})
	t.Run("stopping the server falls back", func(t *testing.T) {
		// Nothing configured changes; the endpoint simply stops answering.
		st, _ := sampleHarness(t, 0, true)
		cfg := layaCfg(layaAuthoritative, "http://127.0.0.1:1")
		before := kernel.Decision{Model: "top-id", Effort: "high", Reason: "r", Unsure: true}
		after, _, ok := st.layaRouting(cfg, before, "task")
		if ok || after != before {
			t.Error("a stopped laya-serve must leave the decision untouched")
		}
	})
}

// Every key the docs tell a user to set must be settable, or the
// instructions are broken. tier_sample is here because 0.65.0 shipped the
// feature and documented the command without whitelisting the key.
func TestDocumentedConfigKeysAreSettable(t *testing.T) {
	for _, key := range []string{
		"mode.laya", "laya.endpoint", "laya.api_key_env", "laya.timeout_ms",
		"mode.tier_sample", "tier_sample.rate",
	} {
		if _, ok := findTunable(key); !ok {
			t.Errorf("%s is documented but not settable via `deadeye config set`", key)
		}
	}
}

// The enum must accept exactly the four rungs and reject anything else, so a
// typo can't write a dead value that silently reads as off.
func TestLayaModeEnumIsTheFourRungs(t *testing.T) {
	tn, ok := findTunable("mode.laya")
	if !ok {
		t.Fatal("mode.laya not settable")
	}
	want := map[string]bool{layaOff: true, layaShadow: true, layaAdvise: true, layaAuthoritative: true}
	if len(tn.allowed) != len(want) {
		t.Fatalf("allowed = %v, want the four rungs", tn.allowed)
	}
	for _, v := range tn.allowed {
		if !want[v] {
			t.Errorf("unexpected allowed value %q", v)
		}
	}
}

func TestLayaStatusAndAgreementRenderWithoutAnEndpoint(t *testing.T) {
	// Both must be safe to run before anything is configured -- that's the
	// first thing /deadeye-laya does.
	out := captureStdout(t, func() { layaStatus(config.Default()) })
	if !strings.Contains(out, "unset") || !strings.Contains(out, "off") {
		t.Errorf("status should report an unset endpoint and off mode:\n%s", out)
	}
	if !strings.Contains(out, "never installs or runs it") {
		t.Errorf("status must not imply deadeye manages laya:\n%s", out)
	}
}

// The tier-sample screen: with Laya agreeing, no paid judge call happens and
// nothing is recorded. This is the cost argument for the screen.
func TestTierSampleLayaScreenSuppressesPaidCallOnAgreement(t *testing.T) {
	st, outPath := sampleHarness(t, 0, true)
	judgeCalls := 0
	prev := judgeFunc
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 0, true }
	t.Cleanup(func() { judgeFunc = prev })

	// Laya says tier 2, matching the routed tier -> agreement -> stop.
	cfg := layaCfg(layaShadow, layaServer(t, `{"answers":{"q":{"choice":"2","answer_confidence":0.9}}}`))
	cfg.Mode.TierSample = "on"
	cfg.Mode.RoutingJudge = "on"
	cfg.TierSample.Rate = 1

	st.maybeSampleTier(cfg, highTierDecision(t, st.cat), st.cat, "shape", "prompt", "sess", t.TempDir())

	if judgeCalls != 0 {
		t.Errorf("judge was called %d times despite Laya agreeing; the screen should have stopped it", judgeCalls)
	}
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("recorded %d outcomes on agreement, want 0", n)
	}
}

// Laya disagreeing escalates to the paid judge, and only a second
// disagreement is recorded -- two independent opinions before a claim.
func TestTierSampleLayaScreenEscalatesOnDisagreement(t *testing.T) {
	st, outPath := sampleHarness(t, 0, true)
	judgeCalls := 0
	prev := judgeFunc
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 0, true }
	t.Cleanup(func() { judgeFunc = prev })

	cfg := layaCfg(layaShadow, layaServer(t, `{"answers":{"q":{"choice":"0","answer_confidence":0.9}}}`))
	cfg.Mode.TierSample = "on"
	cfg.Mode.RoutingJudge = "on"
	cfg.TierSample.Rate = 1

	st.maybeSampleTier(cfg, highTierDecision(t, st.cat), st.cat, "shape", "prompt", "sess", t.TempDir())

	if judgeCalls != 1 {
		t.Errorf("judge called %d times, want exactly 1 confirmation", judgeCalls)
	}
	if n := len(disagreements(t, outPath)); n != 1 {
		t.Errorf("recorded %d outcomes, want 1 confirmed disagreement", n)
	}
}

// A configured-but-silent classifier must not fall back to paying for the
// sample: it was configured precisely to avoid that.
func TestTierSampleSkipsWhenLayaConfiguredButSilent(t *testing.T) {
	st, outPath := sampleHarness(t, 0, true)
	judgeCalls := 0
	prev := judgeFunc
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 0, true }
	t.Cleanup(func() { judgeFunc = prev })

	cfg := layaCfg(layaShadow, "http://127.0.0.1:1") // unreachable
	cfg.Mode.TierSample = "on"
	cfg.Mode.RoutingJudge = "on"
	cfg.TierSample.Rate = 1

	st.maybeSampleTier(cfg, highTierDecision(t, st.cat), st.cat, "shape", "prompt", "sess", t.TempDir())

	if judgeCalls != 0 {
		t.Errorf("judge called %d times; a silent configured classifier should skip, not pay", judgeCalls)
	}
	if n := len(disagreements(t, outPath)); n != 0 {
		t.Errorf("recorded %d outcomes, want 0", n)
	}
}

// With Laya off, the sampler must behave exactly as 0.65.0 did: rate-limited
// and paying for its own samples.
func TestTierSampleUnchangedWithLayaOff(t *testing.T) {
	st, outPath := sampleHarness(t, 1, true)
	cfg := layaCfg(layaOff, "")
	cfg.Mode.TierSample = "on"
	cfg.Mode.RoutingJudge = "on"
	cfg.TierSample.Rate = 3
	d := highTierDecision(t, st.cat)
	for i := 0; i < 6; i++ {
		st.maybeSampleTier(cfg, d, st.cat, "shape", "p"+string(rune('a'+i)), "sess", t.TempDir())
	}
	if n := len(disagreements(t, outPath)); n != 2 {
		t.Errorf("got %d samples from 6 decisions at rate 3, want 2 (unchanged 0.65.0 behavior)", n)
	}
}

// End-to-end through the real hook handler against a stub laya-serve. Unit
// tests cover each piece; this is the only thing that proves the pieces are
// actually wired into decideAgentRouting, that a verdict reaches
// outcomes.jsonl, and that the advisory the user sees is coherent.
func TestDecideAgentRoutingEndToEndWithLaya(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := judgeFunc
	judgeCalls := 0
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 2, true }
	t.Cleanup(func() { judgeFunc = prev; judgeCache.Clear() })
	judgeCache.Clear()

	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, err := json.Marshal(map[string]any{
		"description": "rename a local variable",
		"prompt":      "Rename the local variable tmp to buf in one Go function. Nothing else.",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := hookio.Input{SessionID: "e2e", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}

	cfg := layaCfg(layaAuthoritative, layaServer(t, `{"answers":{"q":{"choice":"0","answer_confidence":0.91}}}`))
	cfg.Mode.RoutingJudge = "on"

	out := decideAgentRouting(in, cfg, state)

	// This layer proves WIRING, not the override: whether Laya gets to
	// change the model depends on the decision being Unsure, which in turn
	// depends on what the six signals made of this particular scope. The
	// override itself is pinned directly in TestLayaRoutingAuthoritative*
	// against a constructed Unsure decision; forcing it through the real
	// signal stack would make this test a hostage to signal tuning.
	if out.HookSpecificOutput.AdditionalContext == "" {
		t.Error("no advisory produced at all")
	}
	// Whatever the routing outcome, a configured Laya must never leave the
	// advisory malformed or half-written.
	if strings.Contains(out.HookSpecificOutput.AdditionalContext, "%!") {
		t.Errorf("advisory has a botched format verb:\n%s", out.HookSpecificOutput.AdditionalContext)
	}

	// The verdict must have reached the outcomes store, with both sides.
	outs, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		t.Fatal(err)
	}
	var v *lessons.Outcome
	for i := range outs {
		if outs[i].Kind == KindLayaVerdict && outs[i].Site == siteJudge {
			v = &outs[i]
		}
	}
	if v == nil {
		t.Fatalf("no laya verdict recorded; got %+v", outs)
	}
	if v.LayaValue != "0" {
		t.Errorf("LayaValue = %q, want 0", v.LayaValue)
	}
	if v.Actual == "" {
		t.Error("Actual is empty -- agreement can't be computed without the tier that shipped")
	}
	_ = judgeCalls
}

// The same path with Laya off must be byte-identical to pre-0.66.0: the
// judge runs, nothing is recorded, and no advisory mentions Laya.
func TestDecideAgentRoutingUnchangedWithLayaOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prev := judgeFunc
	judgeCalls := 0
	judgeFunc = func(string) (int, bool) { judgeCalls++; return 1, true }
	t.Cleanup(func() { judgeFunc = prev; judgeCache.Clear() })
	judgeCache.Clear()

	state := newDaemonState(testCatalogForLessons(), nil)
	toolInput, _ := json.Marshal(map[string]any{"description": "d", "prompt": "an ambiguous under-specified task"})
	in := hookio.Input{SessionID: "off1", ToolName: "Agent", Cwd: t.TempDir(), ToolInput: toolInput}

	cfg := config.Default() // mode.laya off, no endpoint
	cfg.Mode.RoutingJudge = "on"
	out := decideAgentRouting(in, cfg, state)

	if strings.Contains(out.HookSpecificOutput.AdditionalContext, "laya") {
		t.Errorf("advisory mentions laya with it off:\n%s", out.HookSpecificOutput.AdditionalContext)
	}
	outs, _ := lessons.Scan(meta.OutcomesPath())
	for _, o := range outs {
		if o.Kind == KindLayaVerdict {
			t.Error("a laya verdict was recorded with laya off")
		}
	}
	_ = judgeCalls // the judge only runs on an Unsure decision; not asserted here
}

// runLaya must honor the env kill switch, not just config.json. A status
// command that says "shadow" while DEADEYE_LAYA=off has disabled it is
// worse than no status at all -- and config.Load() applies no kill
// switches, which is the trap this pins.
func TestLayaStatusHonorsEnvKillSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".deadeye"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJSON := `{"mode":{"laya":"authoritative"},"laya":{"endpoint":"http://127.0.0.1:8000","timeout_ms":1500}}`
	if err := os.WriteFile(filepath.Join(home, ".deadeye", "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// Matched on the "mode <value>" line only: the enum hint printed beside
	// it lists every rung, so a naive Contains("authoritative") is true even
	// when the mode is off.
	modeLine := func(out string) string {
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) >= 2 && f[0] == "mode" {
				return f[1]
			}
		}
		return ""
	}

	out := captureStdout(t, func() { runLaya([]string{"status"}) })
	if got := modeLine(out); got != layaAuthoritative {
		t.Fatalf("baseline mode = %q, want authoritative:\n%s", got, out)
	}

	t.Setenv("DEADEYE_LAYA", "off")
	out = captureStdout(t, func() { runLaya([]string{"status"}) })
	if got := modeLine(out); got != layaOff {
		t.Errorf("mode = %q under DEADEYE_LAYA=off, want off:\n%s", got, out)
	}
}

// `deadeye laya test` must not tell someone already on shadow to set shadow.
func TestLayaTestHintIsModeAware(t *testing.T) {
	srv := layaServer(t, `{"answers":{"q":{"choice":"0","answer_confidence":0.9}}}`)

	offCfg := layaCfg(layaOff, srv)
	out := captureStdout(t, func() { layaTest(offCfg) })
	if !strings.Contains(out, "config set mode.laya shadow") {
		t.Errorf("with laya off, the hint should offer shadow:\n%s", out)
	}

	shadowCfg := layaCfg(layaShadow, srv)
	out = captureStdout(t, func() { layaTest(shadowCfg) })
	if strings.Contains(out, "config set mode.laya shadow") {
		t.Errorf("already on shadow -- must not tell the user to set it again:\n%s", out)
	}
	if !strings.Contains(out, "already shadow") {
		t.Errorf("should acknowledge the current rung:\n%s", out)
	}
}
