// Package signals implements PLAN.md §3.1: providers that turn a task into
// Evidence the kernel can grid-search over. Providers are optional and
// degradable -- the plugin works with zero of them (the kernel just stays
// at its conservative ceiling, per INV-1).
package signals

import "context"

// Scope is what a provider assesses.
type Scope struct {
	Prompt       string
	Files        []string
	Repo         string
	SessionMode  string
	SubagentType string // the Agent tool's subagent_type, when known
}

// Evidence is one provider's estimate. Facts is free-form -- providers
// attach whatever supporting detail is cheap to compute; the kernel
// doesn't require any particular key.
type Evidence struct {
	Provider   string
	Complexity float64 // 0..1
	Confidence float64 // 0..1 -- how much the provider trusts its own estimate
	Facts      map[string]any
}

// Signal is a single evidence provider. An error means "skip this
// provider for this call" -- never treated as low-complexity evidence.
type Signal interface {
	Name() string
	Assess(ctx context.Context, s Scope) (Evidence, error)
}

// quietSkipper is an optional extension a Signal can implement to mark
// itself as a BONUS signal, not a core one: a skip is the NORMAL outcome
// for it (e.g. SubagentKind only ever fires for one specific subagent_type
// value -- everything else, including the common "general-purpose", skips
// by design, not because information is missing). AssessAll's skip penalty
// below exists to catch a genuine information GAP (filescope/gitchurn/
// testpresence going quiet on a clean tree); folding a bonus signal's
// routine, expected absence into that same penalty would mean nearly every
// real decision permanently hits the zero-confidence floor and can never
// downshift -- caught live: adding SubagentKind to Builtins() without this
// broke TestDownshiftIsReachableThroughRealProviders, a well-tested,
// single-file, freshly-committed task that should never have stopped
// downshifting.
type quietSkipper interface {
	QuietSkip() bool
}

// AssessAll runs every provider and collects whatever succeeds, degrading
// per PLAN.md §3.1. A skipped provider is recorded as an explicit
// zero-confidence "unknown" Evidence item rather than silently dropped --
// caught live: on a clean working tree, three of the five builtins error
// out (nothing to assess), leaving only promptshape's evidence. The
// kernel's own empty-evidence check only catches a wholly EMPTY set, not
// "only the weakest signal had anything to say" -- so a vague-sounding but
// keyword-free prompt against a clean tree could clear the confidence
// threshold on promptshape's evidence ALONE and downshift to the cheapest
// tier for an architecture-sized task, purely because committing your
// work made every other signal go quiet. Emitting the gap as a
// zero-confidence item routes it through the kernel's existing
// min-confidence rule (a single low-confidence item always blocks
// downshift) with no change to kernel.Decide itself, and makes the gap
// visible in /deadeye-route instead of looking like agreement.
func AssessAll(ctx context.Context, s Scope, providers []Signal) []Evidence {
	var out []Evidence
	var skipped []string
	for _, p := range providers {
		e, err := p.Assess(ctx, s)
		if err != nil {
			if qs, ok := p.(quietSkipper); ok && qs.QuietSkip() {
				continue // a bonus signal had nothing extra to add -- not a gap
			}
			skipped = append(skipped, p.Name())
			continue
		}
		out = append(out, e)
	}
	if len(skipped) > 0 {
		out = append(out, Evidence{
			Provider:   "unknown",
			Complexity: 0,
			Confidence: 0,
			Facts:      map[string]any{"skipped": skipped},
		})
	}
	return out
}

// Builtins is the no-dependency provider set from PLAN.md §3.1.
func Builtins() []Signal {
	return []Signal{
		PromptShape{},
		FileScope{},
		GitChurn{},
		TestPresence{},
		TaskSpecificity{},
		SubagentKind{},
	}
}
