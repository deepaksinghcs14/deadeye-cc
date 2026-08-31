package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"sync"
	"time"
)

// The optional AI routing judge (mode.routing_judge=on). When the cheap
// signals can't confidently place a subtask (kernel.Decision.Unsure), this
// classifies its complexity into a tier with a real model call -- more
// accurate than keyword heuristics on exactly the ambiguous cases. It shells
// to `claude -p` (haiku), reusing the user's Claude login: no API key. It is
// bounded, cached, and fail-open -- any error leaves the heuristic decision
// untouched. Off by default (it breaks the zero-network promise).

const judgeTimeout = 6 * time.Second

const judgePrompt = `You are a model-routing classifier for a coding agent. Read the subtask below and reply with ONLY a single digit and nothing else:
0 = mechanical work (small edits, search, lookups, formatting, classification, boilerplate)
1 = standard multi-file coding (most real tasks)
2 = deep architecture, tricky/subtle debugging, or security-critical work
Subtask: `

// judgeFunc is the classifier, a package var so tests stub it without a real
// model call. Returns (tier, true) or (_, false) on any failure.
var judgeFunc = judgeTierClaude

var judgeCache sync.Map // task-hash -> tier (int)

// judgeTierCached classifies a task into tier 0/1/2, caching by task text so an
// identical subtask isn't re-judged (retries, repeated spawns).
func judgeTierCached(task string) (int, bool) {
	if task == "" {
		return 0, false
	}
	sum := sha256.Sum256([]byte(task))
	key := hex.EncodeToString(sum[:8])
	if v, ok := judgeCache.Load(key); ok {
		return v.(int), true
	}
	tier, ok := judgeFunc(task)
	if ok {
		judgeCache.Store(key, tier)
	}
	return tier, ok
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
	cmd := exec.CommandContext(ctx, "claude", "-p", judgePrompt+task, "--model", "haiku")
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
