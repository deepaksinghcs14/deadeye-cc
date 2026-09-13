package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/kernel"
)

// The AI routing judge (mode.routing_judge, on by default). When the cheap
// signals can't confidently place a subtask (kernel.Decision.Unsure -- the
// majority of real routing decisions, per this repo's own decision log),
// this classifies its complexity into a tier with a real model call -- more
// accurate than keyword heuristics on exactly the ambiguous cases. It shells
// to `claude -p` (sonnet -- haiku's classifications were unreliable enough on
// real ambiguous tasks that a judge run in production would be judging
// wrong more often than the heuristic it's meant to improve on), reusing
// the user's Claude login: no API key. It is bounded, cached, and
// fail-open -- any error leaves the heuristic decision untouched. This is a
// deliberate trade of the zero-network default for accuracy on the
// genuinely ambiguous cases; off switches back to pure heuristics.

// judgeTimeout bounds the `claude -p` subprocess. Measured live against
// haiku, a bare, standalone `claude -p --model haiku` cold start took 5.6s
// on ordinary hardware -- a 6s budget left near-zero margin and silently
// fell back to the heuristic decision (fail-open) on 3 of 4 real test
// calls. Sonnet's cold start runs longer than haiku's; 30s gives real
// headroom so the now-default judge doesn't fail open more often than it
// actually answers.
const judgeTimeout = 30 * time.Second

// judgePrompt is calibrated against benchmarks/routing's measured ground
// truth, not intuition. Two defects in the original wording showed up there:
// it said tier 1 was "most real tasks" (an explicit prior that pushed
// everything to sonnet), and it described tier 0 by EDIT SIZE ("small edits,
// boilerplate"), which left no home for "write one self-contained function
// from a complete spec". So the judge graded difficulty by how advanced the
// topic sounded -- routing SemVer precedence and a race-safe concurrent
// counter to sonnet 5/5 and 4/5, when haiku passed both against a hidden
// test. Scope and specification predict tier far better than subject matter;
// the tie-break is scoped to the 0/1 boundary, where over-provisioning was
// measured, and deliberately leaves the 1/2 boundary alone -- guessing low on
// security-critical or architectural work is the expensive direction to be
// wrong in.
const judgePrompt = `You are a model-routing classifier for a coding agent. Read the subtask below and reply with ONLY a single digit and nothing else.

Judge by SCOPE and SPECIFICATION, not by how advanced the topic sounds. A self-contained, fully-specified piece of work is tier 0 even when the algorithm is fiddly -- parsing, precedence rules, and concurrency primitives are routine when the spec is complete.

0 = one self-contained, clearly specified unit of work: a single function, file, or package written from a complete spec; a mechanical edit; search; lookup; formatting; classification. Fiddly-but-specified belongs here.
1 = work that spans or modifies existing code, or whose requirements must be inferred: multi-file changes, editing unfamiliar code, integrating with an existing system, an under-specified ask.
2 = deep architecture decisions, subtle or tricky debugging, or security-critical work where a wrong answer is expensive.

If torn between 0 and 1, choose 0.
Subtask: `

// judgeFunc is the classifier, a package var so tests stub it without a real
// model call. Returns (tier, true) or (_, false) on any failure.
var judgeFunc = judgeTierClaude

var judgeCache sync.Map // task-hash -> tier (int)

// judgeInflight holds task-hashes with a background judge call running, so
// N repeated spawns of the same subtask start ONE `claude -p`, not N.
var judgeInflight sync.Map // task-hash -> struct{}

func judgeKey(task string) string {
	sum := sha256.Sum256([]byte(task))
	return hex.EncodeToString(sum[:8])
}

// judgeTierCached classifies a task into tier 0/1/2, caching by task text so an
// identical subtask isn't re-judged (retries, repeated spawns). Synchronous:
// waits for the model call. Only the dry-run /deadeye-route path may wait --
// see judgeTierAsync for why the hook path must not.
func judgeTierCached(task string) (int, bool) {
	if task == "" {
		return 0, false
	}
	key := judgeKey(task)
	if v, ok := judgeCache.Load(key); ok {
		return v.(int), true
	}
	tier, ok := judgeFunc(task)
	if ok {
		judgeCache.Store(key, tier)
	}
	return tier, ok
}

// judgeTierAsync is the hook-path judge. A PreToolUse hook has ~200ms of
// client deadline (hook.go requestTimeout) and a 5s hook timeout; the judge
// is a `claude -p` call with a 30s budget. Running it inline meant the
// client had already failed open to {} by the time the verdict existed --
// with routing_judge on (the default) and a committed tree (every decision
// Unsure), NO routing advice was ever delivered on a first-seen subtask,
// not even the heuristic one (caught by a live repro: first call {} in
// 0.2s, identical second call answered from cache). So: serve the cached
// verdict if there is one; otherwise start ONE background judge for this
// task and return ok=false immediately, so the caller delivers the
// heuristic decision now. The next call with the same prompt -- the retry
// or repeated spawn the cache was built for -- gets the judge's verdict.
// The goroutine holds no daemon state, and the `claude -p` subprocess
// already carries its own judgeTimeout context.
func judgeTierAsync(task string) (tier int, ok, started bool) {
	if task == "" {
		return 0, false, false
	}
	key := judgeKey(task)
	if v, ok := judgeCache.Load(key); ok {
		return v.(int), true, false
	}
	if _, running := judgeInflight.LoadOrStore(key, struct{}{}); running {
		return 0, false, false
	}
	go func() {
		defer judgeInflight.Delete(key)
		if t, ok := judgeFunc(task); ok {
			judgeCache.Store(key, t)
		}
	}()
	return 0, false, true
}

// applyRoutingJudge runs the optional AI judge against decision when the
// cheap signals couldn't confidently place the task, folding its
// classification back in. Shared by the real routing path
// (decideAgentRouting, wait=false: never block a tool call on a model
// call) and the dry-run explain path (runRoute, wait=true: a CLI that can
// afford to wait for the verdict) so the two can't drift in how they
// resolve a verdict -- they were drifting when each kept its own copy.
func applyRoutingJudge(cfg config.Config, decision kernel.Decision, cat catalog.Catalog, prompt string, wait bool) kernel.Decision {
	if cfg.Mode.RoutingJudge != "on" || !decision.Unsure {
		return decision
	}
	var tier int
	var ok bool
	if wait {
		tier, ok = judgeTierCached(prompt)
	} else {
		var started bool
		tier, ok, started = judgeTierAsync(prompt)
		if !ok && started {
			// Heuristic advice goes out now, labelled: the log row and
			// /deadeye-stats must not count this as a judged answer.
			decision.Reason += " (judge pending)"
		}
	}
	if !ok {
		return decision
	}
	m, ok := judgeTierToModel(cat, tier)
	if !ok {
		return decision
	}
	decision.Model = m
	decision.Effort = []string{"low", "medium", "high"}[tier]
	decision.Reason = fmt.Sprintf("AI judge classified this subtask as tier %d", tier)
	// The judge resolved what the heuristics couldn't -- clear the
	// thin-evidence flags so downstream code doesn't treat a
	// judge-classified decision as still unresolved.
	decision.Confidence = 1
	decision.Unsure = false
	return decision
}

// judgeTierToModel resolves the judge's 0/1/2 qualitative bucket
// (mechanical/standard/hard -- see judgePrompt) to a concrete model the
// same way kernel.Decide's own ceilings do: tier 0 is always the
// catalog's cheapest model; 1 and 2 go through UnsureCeiling/HighCeiling
// (an explicit role tag if the catalog has one, else the historical tier
// 1/2 fallback). Sharing this resolution with the kernel means the judge
// and the deterministic path can never disagree about what "the capable
// middle" or "the high ceiling" concretely means in this catalog -- the
// judge's own vocabulary stays exactly 3-way regardless of how many tiers
// the catalog actually has; it's a qualitative judgment, not a tier count.
func judgeTierToModel(cat catalog.Catalog, tier int) (string, bool) {
	switch tier {
	case 0:
		m, ok := cat.Cheapest()
		return m.ID, ok
	case 1:
		m, ok := cat.UnsureCeiling()
		return m.ID, ok
	case 2:
		m, ok := cat.HighCeiling()
		return m.ID, ok
	}
	return "", false
}

// judgeTierClaude runs the classification through `claude -p`. DEADEYE_JUDGE=1
// is set so the nested session's own deadeye hooks no-op (see deadeye-hook.sh)
// -- no recursion, no cost from the judge session itself.
func judgeTierClaude(task string) (int, bool) {
	if _, err := exec.LookPath("claude"); err != nil {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", judgePrompt+task, "--model", "sonnet")
	cmd.Env = append(os.Environ(), "DEADEYE_JUDGE=1")
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	return parseTier(string(out))
}

// parseTier reads the first 0/1/2 from the judge's output -- robust to a stray
// newline or the model prefixing a word before the digit.
func parseTier(s string) (int, bool) {
	for _, r := range s {
		switch r {
		case '0':
			return 0, true
		case '1':
			return 1, true
		case '2':
			return 2, true
		}
	}
	return 0, false
}
