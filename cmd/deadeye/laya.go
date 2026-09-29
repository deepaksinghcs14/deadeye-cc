package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
	"github.com/deepaksinghcs14/deadeye-cc/internal/laya"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// The rungs of mode.laya. See config.Modes.Laya for what each one means.
const (
	layaOff           = "off"
	layaShadow        = "shadow"
	layaAdvise        = "advise"
	layaAuthoritative = "authoritative"
)

// KindLayaVerdict marks a recorded Laya answer beside what deadeye did.
const KindLayaVerdict = "laya-verdict"

// The call sites, as recorded in Outcome.Site.
const (
	siteJudge        = "judge"
	siteComplexity   = "complexity"
	sitePlanGate     = "plan-gate"
	siteWorkflowHint = "workflow-hint"
	siteTierSample   = "tier-sample"
	siteFixShaped    = "fix-shaped"
)

// layaEnabled reports whether Laya should be called at all.
func layaEnabled(cfg config.Config) bool {
	switch cfg.Mode.Laya {
	case layaShadow, layaAdvise, layaAuthoritative:
		return cfg.Laya.Endpoint != ""
	}
	return false
}

// layaDecides reports whether Laya's answer may actually change behavior.
// Only the top rung: shadow and advise both record without acting, which is
// what makes promoting up the ladder an evidence-based decision rather than
// a leap.
func layaDecides(cfg config.Config) bool {
	return cfg.Mode.Laya == layaAuthoritative && cfg.Laya.Endpoint != ""
}

// layaClient builds a client from config. Returns nil whenever Laya is off
// or unconfigured -- a nil *laya.Client answers nothing, so call sites hold
// one unconditionally instead of branching on config themselves.
func layaClient(cfg config.Config) *laya.Client {
	if !layaEnabled(cfg) {
		return nil
	}
	key := ""
	if cfg.Laya.APIKeyEnv != "" {
		key = os.Getenv(cfg.Laya.APIKeyEnv)
	}
	return laya.New(cfg.Laya.Endpoint, key, time.Duration(cfg.Laya.TimeoutMS)*time.Millisecond)
}

// layaTimeout bounds one call from a hook, independent of the client's own
// deadline, so a call site can be stricter than the configured budget.
func layaTimeout(cfg config.Config) time.Duration {
	if cfg.Laya.TimeoutMS > 0 {
		return time.Duration(cfg.Laya.TimeoutMS) * time.Millisecond
	}
	return laya.DefaultTimeout
}

// recordLayaVerdict stores what Laya said beside what deadeye did. This is
// the whole point of the shadow rung: agreement is computable later without
// having changed anything now.
func (d *daemonState) recordLayaVerdict(site, shape, layaValue, actual, model, sessionID, cwd string) {
	d.recordOutcome(lessons.Outcome{
		TS:        nowRFC3339(),
		SessionID: sessionID,
		Surface:   lessons.SurfaceRouting,
		TaskShape: shape,
		Model:     model,
		Kind:      KindLayaVerdict,
		Site:      site,
		LayaValue: layaValue,
		Actual:    actual,
		Repo:      gitutil.ProjectKey(cwd),
	})
}

// tierCriteria mirrors judgePrompt's own three-way vocabulary verbatim in
// intent. Keeping one wording for both the `claude -p` judge and Laya is
// what makes `/deadeye-stats laya`'s agreement number mean anything: a
// disagreement should be the classifier's, not an artifact of two
// differently-worded questions.
var tierCriteria = map[string]string{
	"0": "one self-contained, clearly specified unit of work: a single function, file, or package written from a complete spec; a mechanical edit; search; lookup; formatting; classification. Fiddly-but-specified belongs here.",
	"1": "work that spans or modifies existing code, or whose requirements must be inferred: multi-file changes, editing unfamiliar code, integrating with an existing system, an under-specified ask.",
	"2": "deep architecture decisions, subtle or tricky debugging, or security-critical work where a wrong answer is expensive.",
}

const tierInstructions = "Classify how much capability this software subtask needs. If torn between 0 and 1, choose 0."

// layaTier asks Laya for a tier. Returns (-1, false) on anything short of a
// clean, parseable, sufficiently certain answer.
func layaTier(ctx context.Context, c *laya.Client, prompt string) (int, float64, bool) {
	a, ok := c.Choice(ctx, prompt, tierInstructions, tierCriteria)
	if !ok {
		return -1, 0, false
	}
	switch strings.TrimSpace(a.Choice) {
	case "0":
		return 0, a.Certainty(), true
	case "1":
		return 1, a.Certainty(), true
	case "2":
		return 2, a.Certainty(), true
	}
	return -1, a.Certainty(), false
}

// layaYes asks a yes/no proposition and reports whether P(true) clears
// bar. A missing probability reads as "no", never as "yes": an unreachable
// or unsure classifier must not be able to fire a gate on its own.
func layaYes(ctx context.Context, c *laya.Client, text, question string, bar float64) (bool, float64, bool) {
	a, ok := c.YesNo(ctx, text, question)
	if !ok {
		return false, 0, false
	}
	return a.Noul >= bar, a.Noul, true
}

// runLaya backs `deadeye laya <status|health|test>` -- the mechanical steps
// the /deadeye-laya setup skill drives and verifies against. Deliberately
// small: the skill does the explaining, this does the checking.
func runLaya(args []string) {
	cfg := config.Load()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status":
		layaStatus(cfg)
	case "health":
		layaHealth(cfg)
	case "test":
		layaTest(cfg)
	case "agreement":
		layaAgreement(meta.OutcomesPath(), cfg, time.Now())
	default:
		fmt.Fprintln(os.Stderr, "usage: deadeye laya <status|health|test|agreement>")
		os.Exit(2)
	}
}

func layaStatus(cfg config.Config) {
	mode := cfg.Mode.Laya
	if mode == "" {
		mode = layaOff
	}
	fmt.Println("  " + cHead("laya") + cDim("   optional local decision model -- deadeye never installs or runs it"))
	fmt.Println()
	fmt.Printf("  mode        %s %s\n", cValue(mode), cDim("(off · shadow · advise · authoritative)"))
	if cfg.Laya.Endpoint == "" {
		fmt.Printf("  endpoint    %s %s\n", cWarn("unset"), cDim("(set it with: deadeye config set laya.endpoint "+laya.DefaultEndpoint+")"))
	} else {
		fmt.Printf("  endpoint    %s\n", cValue(cfg.Laya.Endpoint))
	}
	fmt.Printf("  timeout     %dms\n", cfg.Laya.TimeoutMS)
	keyState := cDim("not set")
	if cfg.Laya.APIKeyEnv != "" {
		if os.Getenv(cfg.Laya.APIKeyEnv) != "" {
			keyState = cGood("present in $" + cfg.Laya.APIKeyEnv)
		} else {
			keyState = cDim("$" + cfg.Laya.APIKeyEnv + " empty (fine unless laya-serve requires a token)")
		}
	}
	fmt.Printf("  api key     %s\n", keyState)
	fmt.Println()
	switch mode {
	case layaOff:
		fmt.Println("  " + cDim("Off: nothing calls Laya, and every decision behaves exactly as it"))
		fmt.Println("  " + cDim("did before this feature existed."))
	case layaShadow:
		fmt.Println("  " + cGood("Shadow:") + cDim(" Laya is asked and its answers recorded, but nothing acts"))
		fmt.Println("  " + cDim("on them. Compare with /deadeye-stats laya before promoting."))
	case layaAdvise:
		fmt.Println("  " + cGood("Advise:") + cDim(" answers are recorded and shown in decision reasons,"))
		fmt.Println("  " + cDim("but still change no behavior."))
	case layaAuthoritative:
		fmt.Println("  " + cWarn("Authoritative:") + cDim(" Laya's answers are USED. Check"))
		fmt.Println("  " + cDim("/deadeye-stats laya regularly -- untuned accuracy on typed"))
		fmt.Println("  " + cDim("decisions is near chance on the vendor's own eval."))
	}
}

func layaHealth(cfg config.Config) {
	c := laya.New(cfg.Laya.Endpoint, os.Getenv(cfg.Laya.APIKeyEnv), layaTimeout(cfg))
	if c == nil {
		fmt.Println(cWarn("no endpoint configured") + " -- deadeye config set laya.endpoint " + laya.DefaultEndpoint)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		fmt.Printf("%s %s\n", cWarn("unreachable:"), err)
		fmt.Println(cDim("start it with: LAYA_PRELOAD=1 laya-serve"))
		os.Exit(1)
	}
	fmt.Printf("%s %s\n", cGood("reachable:"), c.Endpoint())
}

// layaTest sends one real tier question -- an obviously mechanical task --
// so setup can be verified end to end, and so the answer's certainty is
// visible before anyone promotes Laya up the ladder.
func layaTest(cfg config.Config) {
	c := laya.New(cfg.Laya.Endpoint, os.Getenv(cfg.Laya.APIKeyEnv), 30*time.Second)
	if c == nil {
		fmt.Println(cWarn("no endpoint configured") + " -- deadeye config set laya.endpoint " + laya.DefaultEndpoint)
		os.Exit(1)
	}
	const probe = "Rename the local variable `tmp` to `buf` in one Go function. Nothing else."
	// Generous deadline on purpose: a first call may pay a cold checkpoint
	// load of several seconds, which the hook-path budget would reject.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	start := time.Now()
	tier, certainty, ok := layaTier(ctx, c, probe)
	elapsed := time.Since(start)
	if !ok {
		fmt.Println(cWarn("no usable answer") + cDim(" -- endpoint reachable but the reply wasn't a tier."))
		fmt.Println(cDim("  Check `laya-serve` logs, and that the checkpoint finished loading."))
		os.Exit(1)
	}
	fmt.Printf("%s tier %s, certainty %.2f, %s\n", cGood("answered:"), cValue(fmt.Sprintf("%d", tier)), certainty, elapsed.Round(time.Millisecond))
	fmt.Println(cDim("  A mechanical rename should come back tier 0. If it doesn't, Laya is"))
	fmt.Println(cDim("  working but not accurate on this task yet -- leave mode at shadow."))
	if elapsed > layaTimeout(cfg) {
		fmt.Printf("%s first call took longer than the %dms hook budget; later calls\n", cWarn("note:"), cfg.Laya.TimeoutMS)
		fmt.Println(cDim("  should be faster once the checkpoint is resident. Run this again."))
	}
	fmt.Println()
	fmt.Println(cDim("  Recorded nothing -- this is a probe. Set mode.laya=shadow to start"))
	fmt.Println(cDim("  collecting agreement data: ") + cValue("deadeye config set mode.laya shadow"))
}

// layaRouting asks Laya for a tier BEFORE the `claude -p` judge runs, so
// that on the authoritative rung it can stand in for that call entirely --
// which is the whole cost argument for this integration: the judge is the
// one thing in deadeye that blocks a hook response on a model call.
//
// Returns the decision (possibly updated), Laya's tier, and whether Laya
// answered at all. On the shadow and advise rungs the decision comes back
// untouched apart from, on advise, a note in the visible reason.
func (d *daemonState) layaRouting(cfg config.Config, decision kernel.Decision, prompt string) (kernel.Decision, int, bool) {
	c := layaClient(cfg)
	if c == nil {
		return decision, -1, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
	defer cancel()
	tier, certainty, ok := layaTier(ctx, c, prompt)
	if !ok {
		return decision, -1, false
	}
	switch {
	case layaDecides(cfg) && decision.Unsure:
		// Exactly the case applyRoutingJudge would have paid a model call
		// for. Resolve it locally and let the judge return early.
		if m, mok := judgeTierToModel(d.cat, tier); mok {
			decision.Model = m
			decision.Effort = []string{"low", "medium", "high"}[tier]
			decision.Reason = fmt.Sprintf("laya classified this subtask as tier %d (certainty %.2f)", tier, certainty)
			decision.Confidence = 1
			decision.Unsure = false
		}
	case cfg.Mode.Laya == layaAdvise:
		decision.Reason += fmt.Sprintf(" [laya: tier %d, certainty %.2f -- recorded, not applied]", tier, certainty)
	}
	return decision, tier, ok
}

// gateConfirmBar is the P(true) a gate needs to survive suppression on the
// authoritative rung. Set low on purpose: Laya is being asked to veto a
// heuristic that already fired, so the benefit of the doubt goes to the
// heuristic, and only a confident "no, this doesn't need it" suppresses.
const gateConfirmBar = 0.25

// layaConfirmsGate asks Laya whether a gate the heuristic already fired
// deserves to fire. It returns true (keep the gate) whenever Laya is off,
// unreachable, or unsure -- so a missing classifier can never silence a
// suggestion, only a confident one can.
//
// On shadow and advise the answer is recorded and the gate always kept;
// only the authoritative rung lets it suppress.
func (d *daemonState) layaConfirmsGate(cfg config.Config, site, prompt, question, shape, sessionID, cwd string) bool {
	c := layaClient(cfg)
	if c == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
	defer cancel()
	yes, p, ok := layaYes(ctx, c, prompt, question, gateConfirmBar)
	if !ok {
		return true
	}
	keep := true
	if layaDecides(cfg) {
		keep = yes
	}
	d.recordLayaVerdict(site, shape, fmt.Sprintf("%t(p=%.2f)", yes, p), fmt.Sprintf("%t", keep), "", sessionID, cwd)
	return keep
}

// layaAgreement backs `deadeye laya agreement` and /deadeye-stats laya:
// how often Laya matched what deadeye actually did, per call site.
//
// This is the number that makes the mode ladder an evidence-based choice.
// It is an AGREEMENT rate, not an accuracy rate: on the shadow and advise
// rungs "actual" is whatever the existing mechanism decided, which is
// itself not ground truth. Two classifiers agreeing says they agree.
func layaAgreement(outcomesPath string, cfg config.Config, now time.Time) {
	outs, err := lessons.Scan(outcomesPath)
	if err != nil {
		fmt.Println("deadeye laya agreement:", err)
		return
	}
	type stat struct{ agree, total int }
	per := map[string]*stat{}
	total, agreed := 0, 0
	for _, o := range outs {
		if o.Kind != KindLayaVerdict {
			continue
		}
		if ts, perr := time.Parse(time.RFC3339, o.TS); perr == nil && now.Sub(ts) > judgedTierAge {
			continue
		}
		s := per[o.Site]
		if s == nil {
			s = &stat{}
			per[o.Site] = s
		}
		s.total++
		total++
		if o.LayaValue == o.Actual {
			s.agree++
			agreed++
		}
	}

	mode := cfg.Mode.Laya
	if mode == "" {
		mode = layaOff
	}
	fmt.Println("  " + cHead("deadeye laya") + cDim("   agreement between Laya and what deadeye actually did"))
	fmt.Println()
	fmt.Printf("  mode        %s\n", cValue(mode))
	if cfg.Laya.Endpoint == "" {
		fmt.Printf("  endpoint    %s\n", cWarn("unset"))
	} else {
		fmt.Printf("  endpoint    %s\n", cValue(cfg.Laya.Endpoint))
	}
	fmt.Println()

	if total == 0 {
		fmt.Println("  " + cDim("No Laya verdicts recorded in the last 30 days."))
		switch mode {
		case layaOff:
			fmt.Println("  " + cDim("Laya is off. Set it up with ") + cValue("/deadeye-laya") + cDim(", then:"))
			fmt.Println("  " + cValue("deadeye config set mode.laya shadow"))
		default:
			fmt.Println("  " + cDim("Configured but nothing recorded yet -- either no eligible decision"))
			fmt.Println("  " + cDim("has happened, or the endpoint isn't answering. Check:"))
			fmt.Println("  " + cValue("deadeye laya health"))
		}
		return
	}

	fmt.Printf("  %s   %s of %d verdicts %s\n", cHead("Agreement"),
		cValue(fmt.Sprintf("%.0f%%", 100*float64(agreed)/float64(total))), total,
		cDim("(last 30 days)"))
	fmt.Println()
	fmt.Println("  " + cHead("By call site"))
	sites := make([]string, 0, len(per))
	for s := range per {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	for _, site := range sites {
		s := per[site]
		pct := 100 * float64(s.agree) / float64(s.total)
		label := cGood(fmt.Sprintf("%.0f%%", pct))
		if pct < 60 {
			label = cWarn(fmt.Sprintf("%.0f%%", pct))
		}
		fmt.Printf("    %-16s %s  %d/%d\n", site, label, s.agree, s.total)
	}

	fmt.Println()
	fmt.Println(cDim("  This is AGREEMENT, not accuracy. On shadow and advise, \"actual\" is"))
	fmt.Println(cDim("  whatever the existing mechanism chose -- itself not ground truth, so"))
	fmt.Println(cDim("  a high number means the two concur, not that either is right."))
	fmt.Println(cDim("  Laya's untuned accuracy on typed decisions is near chance on its"))
	fmt.Println(cDim("  vendor's own eval (0.362 vs ~0.33 for a 3-way guess), and no"))
	fmt.Println(cDim("  independent evaluation exists. Treat a low number as a reason not to"))
	fmt.Println(cDim("  promote past shadow, and a high one as necessary but not sufficient."))
	if mode == layaAuthoritative {
		fmt.Println()
		fmt.Println("  " + cWarn("Authoritative:") + cDim(" these answers are changing real decisions."))
	}
}
