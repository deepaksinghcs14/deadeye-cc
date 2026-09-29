package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
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
