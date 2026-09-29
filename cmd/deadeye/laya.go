package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	sitePlanGate     = "plan-gate"
	siteWorkflowHint = "workflow-hint"
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

// layaInFlight bounds concurrent background Laya calls.
//
// A single fan-out can spawn dozens of Agent calls at once, and an
// unbounded `go f()` would point all of them at one laya-serve
// simultaneously -- which is single-process, so they queue, time out
// together, and starve the calls that actually need an answer. Work that
// can't get a slot is dropped rather than queued: these are measurements,
// and a missed sample costs nothing while a backlog costs latency.
var layaInFlight = make(chan struct{}, 4)

// layaAsync is the seam tests replace, so a fire-and-forget call can be
// made synchronous and observed. The real one outlives the hook response
// on purpose -- see layaShadowRecord.
var layaAsync = func(f func()) {
	select {
	case layaInFlight <- struct{}{}:
	default:
		return // at capacity; drop this measurement
	}
	go func() {
		defer func() { <-layaInFlight }()
		f()
	}()
}

// layaClientCache memoizes one *laya.Client per endpoint+key+timeout.
//
// Built fresh per call, every Laya request opened a NEW http.Client and
// therefore a new connection pool: no keep-alive reuse, a fresh TCP (and
// TLS) handshake every time, and a steady drip of sockets in a daemon that
// lives for days. One client per configuration reuses its pool, which is
// most of the difference between 464ms and something a hook can afford.
var (
	layaClientMu    sync.Mutex
	layaClientCache = map[string]*laya.Client{}
)

// layaClient returns the shared client for this config. Returns nil
// whenever Laya is off or unconfigured -- a nil *laya.Client answers
// nothing, so call sites hold one unconditionally instead of branching on
// config themselves.
func layaClient(cfg config.Config) *laya.Client {
	if !layaEnabled(cfg) {
		return nil
	}
	// NOTE: read from THIS process's environment. In the daemon that is the
	// environment the daemon was started with, not the shell the user typed
	// the export in -- config.go says the same thing about kill switches,
	// which is why those travel over the wire instead. A token exported
	// after the daemon started is invisible to it, so laya-serve behind
	// LAYA_API_KEY must have that variable set wherever the daemon is
	// launched from. `deadeye laya status` flags the mismatch it can see.
	key := ""
	if cfg.Laya.APIKeyEnv != "" {
		key = os.Getenv(cfg.Laya.APIKeyEnv)
	}
	timeout := time.Duration(cfg.Laya.TimeoutMS) * time.Millisecond
	// The token is part of the identity but must not be the map key in
	// clear text alongside everything else -- length is enough to
	// distinguish "changed" without holding a second copy of a credential.
	ck := fmt.Sprintf("%s|%d|%d|%s", cfg.Laya.Endpoint, timeout, len(key), cfg.Laya.Checkpoint)
	layaClientMu.Lock()
	defer layaClientMu.Unlock()
	if c, ok := layaClientCache[ck]; ok {
		return c
	}
	c := laya.New(cfg.Laya.Endpoint, key, cfg.Laya.Checkpoint, timeout)
	layaClientCache[ck] = c
	return c
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
func (d *daemonState) recordLayaVerdict(site, shape, layaValue, actual, model, checkpoint, sessionID, cwd string) {
	d.recordOutcome(lessons.Outcome{
		TS:         nowRFC3339(),
		SessionID:  sessionID,
		Surface:    lessons.SurfaceRouting,
		TaskShape:  shape,
		Model:      model,
		Kind:       KindLayaVerdict,
		Site:       site,
		LayaValue:  layaValue,
		Actual:     actual,
		Checkpoint: checkpoint,
		Repo:       gitutil.ProjectKey(cwd),
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
func layaTier(ctx context.Context, c *laya.Client, prompt string) (int, float64, string, bool) {
	a, checkpoint, ok := c.Choice(ctx, prompt, tierInstructions, tierCriteria)
	if !ok {
		return -1, 0, "", false
	}
	switch strings.TrimSpace(a.Choice) {
	case "0":
		return 0, a.Certainty(), checkpoint, true
	case "1":
		return 1, a.Certainty(), checkpoint, true
	case "2":
		return 2, a.Certainty(), checkpoint, true
	}
	return -1, a.Certainty(), checkpoint, false
}

// layaYes asks a yes/no proposition and reports whether P(true) clears
// bar.
//
// An answer carrying no certainty is treated as no answer at all. Without
// that check a service returning `{"answers":{"q":{}}}` -- any JSON
// endpoint that isn't laya-serve -- yields Noul=0 with ok=true, which reads
// as a CONFIDENT "no" and can suppress a gate on the authoritative rung.
func layaYes(ctx context.Context, c *laya.Client, text, question string, bar float64) (bool, float64, string, bool) {
	a, checkpoint, ok := c.YesNo(ctx, text, question)
	if !ok || a.Certainty() <= 0 {
		return false, 0, "", false
	}
	return a.Noul >= bar, a.Noul, checkpoint, true
}

// isLoopbackEndpoint reports whether the configured endpoint points at this
// machine.
//
// Nothing stops a user pointing laya.endpoint at a remote host, and if they
// do, every task description deadeye classifies leaves the machine -- which
// would quietly falsify the one claim this feature is sold on ("nothing
// leaves your machine"). deadeye doesn't forbid it; it's the user's
// endpoint. It does have to SAY so, in status and in doctor, rather than
// letting a privacy promise silently stop being true.
func isLoopbackEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// runLaya backs `deadeye laya <status|health|test>` -- the mechanical steps
// the /deadeye-laya setup skill drives and verifies against. Deliberately
// small: the skill does the explaining, this does the checking.
func runLaya(args []string) {
	// LoadFor with the env switches folded in, not plain Load: DEADEYE_LAYA=off
	// is one of the four documented ways to turn Laya off, and a status
	// command that reports "shadow" while the environment has disabled it is
	// worse than no status at all. (config.Load applies no kill switches --
	// that's the daemon's LoadFor path.)
	cwd, _ := os.Getwd()
	cfg := config.LoadFor(cwd, config.OffSwitches())
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status":
		layaStatus(cfg)
	case "health":
		if code := layaHealth(cfg); code != 0 {
			os.Exit(code)
		}
	case "test":
		if code := layaTest(cfg); code != 0 {
			os.Exit(code)
		}
	case "agreement":
		layaAgreement(meta.OutcomesPath(), cfg, time.Now())
	case "classify":
		if code := layaClassify(cfg, args[1:]); code != 0 {
			os.Exit(code)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: deadeye laya <status|health|test|agreement|classify [--json] <prompt>>")
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
	} else if isLoopbackEndpoint(cfg.Laya.Endpoint) {
		fmt.Printf("  endpoint    %s\n", cValue(cfg.Laya.Endpoint))
	} else {
		fmt.Printf("  endpoint    %s %s\n", cValue(cfg.Laya.Endpoint), cWarn("(not loopback)"))
		fmt.Println("  " + cWarn("            task descriptions will leave this machine to reach it"))
	}
	fmt.Printf("  timeout     %dms\n", layaTimeout(cfg).Milliseconds())
	if cfg.Laya.Checkpoint == "" {
		fmt.Printf("  checkpoint  %s %s\n", cWarn("server's choice"), cDim("(its router picks by language, never typed-decisions)"))
	} else if cfg.Laya.Checkpoint == laya.CheckpointTypedDecisions {
		fmt.Printf("  checkpoint  %s %s\n", cValue(cfg.Laya.Checkpoint), cDim("(the fine-tuned one)"))
	} else {
		fmt.Printf("  checkpoint  %s %s\n", cWarn(cfg.Laya.Checkpoint), cDim("(not the fine-tuned typed-decisions checkpoint)"))
	}
	keyState := cDim("not set")
	if cfg.Laya.APIKeyEnv != "" {
		if os.Getenv(cfg.Laya.APIKeyEnv) != "" {
			keyState = cGood("present in $"+cfg.Laya.APIKeyEnv) +
				cDim("  (the daemon only sees it if it was exported before the daemon started)")
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

// layaHealth returns a process exit code rather than calling os.Exit, so
// the behavior is testable without killing the test binary.
func layaHealth(cfg config.Config) int {
	c := laya.New(cfg.Laya.Endpoint, os.Getenv(cfg.Laya.APIKeyEnv), cfg.Laya.Checkpoint, layaTimeout(cfg))
	if c == nil {
		fmt.Println(cWarn("no endpoint configured") + " -- deadeye config set laya.endpoint " + laya.DefaultEndpoint)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		fmt.Printf("%s %s\n", cWarn("unreachable:"), err)
		fmt.Println(cDim("start it with: LAYA_PRELOAD=1 laya-serve"))
		return 1
	}
	fmt.Printf("%s %s\n", cGood("reachable:"), c.Endpoint())
	return 0
}

// layaTest sends one real tier question -- an obviously mechanical task --
// so setup can be verified end to end, and so the answer's certainty is
// visible before anyone promotes Laya up the ladder.
func layaTest(cfg config.Config) int {
	c := laya.New(cfg.Laya.Endpoint, os.Getenv(cfg.Laya.APIKeyEnv), cfg.Laya.Checkpoint, 30*time.Second)
	if c == nil {
		fmt.Println(cWarn("no endpoint configured") + " -- deadeye config set laya.endpoint " + laya.DefaultEndpoint)
		return 1
	}
	const probe = "Rename the local variable `tmp` to `buf` in one Go function. Nothing else."
	// Generous deadline on purpose: a first call may pay a cold checkpoint
	// load of several seconds, which the hook-path budget would reject.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	start := time.Now()
	tier, certainty, checkpoint, ok := layaTier(ctx, c, probe)
	elapsed := time.Since(start)
	if !ok {
		// Distinguish "nothing is listening" from "answered, but not with a
		// tier" -- reporting the endpoint as reachable when the connection
		// was refused sends people to read laya-serve logs that don't exist.
		hctx, hcancel := context.WithTimeout(context.Background(), 3*time.Second)
		herr := c.Health(hctx)
		hcancel()
		if herr != nil {
			fmt.Printf("%s %v\n", cWarn("unreachable:"), herr)
			fmt.Println(cDim("  start it with: LAYA_PRELOAD=1 laya-serve"))
			return 1
		}
		fmt.Println(cWarn("no usable answer") + cDim(" -- the endpoint answered, but not with a tier."))
		fmt.Println(cDim("  Check `laya-serve` logs, that the checkpoint finished loading, and"))
		fmt.Println(cDim("  that this endpoint is actually laya-serve and not another service."))
		return 1
	}
	fmt.Printf("%s tier %s, certainty %.2f, %s\n", cGood("answered:"), cValue(fmt.Sprintf("%d", tier)), certainty, elapsed.Round(time.Millisecond))
	// Which weights answered is the difference between 0.766 and 0.362 on
	// exactly this kind of question, so setup has to show it rather than
	// leave the user assuming they got what they asked for.
	switch {
	case checkpoint == "":
		fmt.Println("  " + cDim("checkpoint: not reported by the server"))
	case checkpoint == laya.CheckpointTypedDecisions:
		fmt.Println("  " + cGood("checkpoint: "+checkpoint) + cDim("  (the fine-tuned one -- correct for deadeye)"))
	default:
		fmt.Println("  " + cWarn("checkpoint: "+checkpoint) + cDim("  -- NOT the fine-tuned typed-decisions one."))
		fmt.Println("  " + cDim("  Restart laya-serve with LAYA_MODELS=english,typed-decisions"))
		fmt.Println("  " + cDim("  (base weights score 0.362 on typed decisions, against 0.766)"))
	}
	fmt.Println(cDim("  A mechanical rename should come back tier 0. If it doesn't, Laya is"))
	fmt.Println(cDim("  working but not accurate on this task yet -- leave mode at shadow."))
	if elapsed > layaTimeout(cfg) {
		fmt.Printf("%s first call took longer than the %dms hook budget; later calls\n", cWarn("note:"), layaTimeout(cfg).Milliseconds())
		fmt.Println(cDim("  should be faster once the checkpoint is resident. Run this again."))
	}
	fmt.Println()
	// The next step depends on where the ladder already is -- telling someone
	// already on shadow to "set mode.laya=shadow" reads as the tool not
	// knowing its own state.
	if cfg.Mode.Laya == layaOff || cfg.Mode.Laya == "" {
		fmt.Println(cDim("  Recorded nothing -- this is a probe. Start collecting agreement data:"))
		fmt.Println("  " + cValue("deadeye config set mode.laya shadow"))
	} else {
		fmt.Printf("%s\n", cDim("  Recorded nothing -- this is a probe. mode.laya is already "+cfg.Mode.Laya+";"))
		fmt.Println(cDim("  real decisions are what populate ") + cValue("/deadeye-stats laya") + cDim("."))
	}
	return 0
}

// layaRouting asks Laya for a tier BEFORE the `claude -p` judge runs, so
// that on the authoritative rung it can stand in for that call entirely --
// which is the whole cost argument for this integration: the judge is the
// one thing in deadeye that blocks a hook response on a model call.
//
// Returns the decision (possibly updated), Laya's tier, and whether Laya
// answered at all. On the shadow and advise rungs the decision comes back
// untouched apart from, on advise, a note in the visible reason.
func (d *daemonState) layaRouting(cfg config.Config, decision kernel.Decision, prompt, shape, sessionID, cwd string) (kernel.Decision, int, string, bool) {
	c := layaClient(cfg)
	if c == nil {
		return decision, -1, "", false
	}
	// Shadow changes nothing, so blocking a real tool call on it would buy
	// measurement with the user's latency -- against a hook budget (INV-8)
	// the whole product is built around. Fire and forget; the caller
	// records nothing and the goroutine does it instead, using the decision
	// as it stands (shadow never alters it, so this snapshot is accurate).
	//
	// advise and authoritative DO stay inline: advise surfaces the answer in
	// the reason the user reads, and authoritative needs it to decide. Both
	// are explicit opt-ins to paying that latency.
	if cfg.Mode.Laya == layaShadow {
		// Nothing here: shadow is asked AFTER the judge has run, by
		// layaShadowRecord below. Scoring against the decision as it stands
		// now would compare Laya to a pre-judge guess the judge then
		// overrode -- systematically marking Laya wrong whenever it agreed
		// with the judge rather than the heuristics.
		return decision, -1, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
	defer cancel()
	tier, certainty, checkpoint, ok := layaTier(ctx, c, prompt)
	if !ok {
		return decision, -1, "", false
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
	return decision, tier, checkpoint, ok
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
	ask := func() (bool, string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
		defer cancel()
		yes, _, checkpoint, ok := layaYes(ctx, c, prompt, question, gateConfirmBar)
		return yes, checkpoint, ok
	}
	// Below the authoritative rung the answer cannot change anything, so
	// asking inline would add up to a full timeout to the user's own turn
	// (this runs on UserPromptSubmit) purely to record a number. Measure
	// off the critical path instead.
	if !layaDecides(cfg) {
		layaAsync(func() {
			if yes, checkpoint, ok := ask(); ok {
				d.recordLayaVerdict(site, shape, fmt.Sprintf("%t", yes), "true", "", checkpoint, sessionID, cwd)
			}
		})
		return true
	}
	yes, checkpoint, ok := ask()
	if !ok {
		return true
	}
	// Actual is always "true" because this function is only reached when the
	// heuristic already fired -- that IS what deadeye would have done. So the
	// number measures "how often Laya agrees with the heuristic", identically
	// on every rung.
	//
	// Recording `yes` on both sides instead (as a first attempt did) makes
	// authoritative tautologically 100%: on that rung Laya IS the decision,
	// so scoring it against itself would flatter it forever on the very
	// report the ladder is promoted on.
	d.recordLayaVerdict(site, shape, fmt.Sprintf("%t", yes), "true", "", checkpoint, sessionID, cwd)
	return yes
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
	// Which weights answered, counted separately: base and fine-tuned
	// differ by more than two to one on exactly this kind of question, so a
	// rate that silently blends them is a number about neither.
	checkpoints := map[string]int{}
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
		if o.Checkpoint != "" {
			checkpoints[o.Checkpoint]++
		}
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
	} else if isLoopbackEndpoint(cfg.Laya.Endpoint) {
		fmt.Printf("  endpoint    %s\n", cValue(cfg.Laya.Endpoint))
	} else {
		fmt.Printf("  endpoint    %s %s\n", cValue(cfg.Laya.Endpoint), cWarn("(not loopback)"))
		fmt.Println("  " + cWarn("            task descriptions will leave this machine to reach it"))
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

	if len(checkpoints) > 0 {
		fmt.Println()
		fmt.Println("  " + cHead("Checkpoint"))
		names := make([]string, 0, len(checkpoints))
		for n := range checkpoints {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			label := cValue(n)
			if n != laya.CheckpointTypedDecisions {
				label = cWarn(n) + cDim("  (not the fine-tuned one -- 0.362 vs 0.766 on typed decisions)")
			}
			fmt.Printf("    %-18s %d verdict(s)\n", label, checkpoints[n])
		}
		if len(names) > 1 {
			fmt.Println("  " + cWarn("  Mixed checkpoints in one window:") + cDim(" the combined rate above is a"))
			fmt.Println("  " + cDim("  number about neither. Serve one checkpoint and let it re-accumulate."))
		}
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

// layaShadowRecord asks Laya about a decision that has already been made
// and records the answer beside it, off the critical path.
//
// Called AFTER the judge so "actual" is the tier that really shipped -- the
// same rule decide.go follows for the other rungs. Comparing against the
// pre-judge decision instead biases the shadow numbers downward exactly
// when Laya is RIGHT (it agrees with the judge, which then overrode the
// heuristic guess), which would be the worst possible error in the data the
// trust ladder is promoted on.
func (d *daemonState) layaShadowRecord(cfg config.Config, decision kernel.Decision, prompt, shape, sessionID, cwd string) {
	if cfg.Mode.Laya != layaShadow {
		return
	}
	c := layaClient(cfg)
	if c == nil {
		return
	}
	tier, tok := d.cat.TierFor(decision.Model)
	if !tok {
		return // no comparable tier; recording one would invent a disagreement
	}
	model := decision.Model
	layaAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), layaTimeout(cfg))
		defer cancel()
		got, _, checkpoint, ok := layaTier(ctx, c, prompt)
		if !ok {
			return
		}
		// The SESSION's cwd, carried into the closure: ProjectKey("") would
		// resolve against the daemon's own working directory and file every
		// verdict under the wrong repo.
		d.recordLayaVerdict(siteJudge, shape, strconv.Itoa(got), strconv.Itoa(tier), model, checkpoint, sessionID, cwd)
	})
}

// layaClassify asks Laya to tier one arbitrary prompt and prints the result,
// optionally as JSON.
//
// It exists so benchmarks/routing/laya-probe.sh measures the EXACT question
// production asks -- same instructions, same criteria, same checkpoint --
// rather than a copy in a shell script that drifts the moment either side is
// edited. A benchmark measuring a slightly different prompt than the product
// sends is worse than no benchmark, because it looks authoritative.
func layaClassify(cfg config.Config, args []string) int {
	asJSON := false
	var prompt []string
	for _, a := range args {
		if a == "--json" {
			asJSON = true
			continue
		}
		prompt = append(prompt, a)
	}
	text := strings.TrimSpace(strings.Join(prompt, " "))
	if text == "" {
		fmt.Fprintln(os.Stderr, "usage: deadeye laya classify [--json] <prompt>")
		return 2
	}
	// Deliberately generous, unlike the hook path's budget: a benchmark
	// wants the answer even when a cold checkpoint load costs seconds, and
	// it measures the latency rather than racing it.
	c := laya.New(cfg.Laya.Endpoint, os.Getenv(cfg.Laya.APIKeyEnv), cfg.Laya.Checkpoint, 60*time.Second)
	if c == nil {
		fmt.Fprintln(os.Stderr, "no laya endpoint configured")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	start := time.Now()
	tier, certainty, checkpoint, ok := layaTier(ctx, c, text)
	ms := time.Since(start).Milliseconds()
	if !ok {
		if asJSON {
			fmt.Printf(`{"ok":false,"ms":%d}`+"\n", ms)
		} else {
			fmt.Println(cWarn("no usable answer"))
		}
		return 1
	}
	if asJSON {
		b, _ := json.Marshal(map[string]any{
			"ok": true, "tier": tier, "certainty": certainty,
			"checkpoint": checkpoint, "ms": ms,
		})
		fmt.Println(string(b))
		return 0
	}
	fmt.Printf("tier %d  certainty %.2f  checkpoint %s  %dms\n", tier, certainty, checkpoint, ms)
	return 0
}
