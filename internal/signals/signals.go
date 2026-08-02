// Package signals implements PLAN.md §3.1: providers that turn a task into
// Evidence the kernel can grid-search over. Providers are optional and
// degradable -- the plugin works with zero of them (the kernel just stays
// at its conservative ceiling, per INV-1).
package signals

import "context"

// Scope is what a provider assesses.
type Scope struct {
	Prompt      string
	Files       []string
	Repo        string
	SessionMode string
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

// AssessAll runs every provider and collects whatever succeeds. Errors are
// dropped silently (degradable per PLAN.md §3.1) -- a provider that can't
// answer contributes nothing, which is exactly what INV-1 wants: it's
// evidence-absence, not zero-complexity evidence.
func AssessAll(ctx context.Context, s Scope, providers []Signal) []Evidence {
	var out []Evidence
	for _, p := range providers {
		e, err := p.Assess(ctx, s)
		if err != nil {
			continue
		}
		out = append(out, e)
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
	}
}
