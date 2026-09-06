package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/usage"
)

// runAudit backs the /deadeye-stats slash command: everything it reports
// comes straight from the decision log, per PLAN.md's "no fabricated
// numbers" rule (§12.1) -- no estimated aggregate is presented as measured.
func runAudit() {
	records, err := logstore.Scan(meta.LogPath())
	if err != nil {
		fmt.Println("deadeye audit:", err)
		return
	}
	if len(records) == 0 {
		fmt.Println("No decisions logged yet -- run a session with the plugin active, then retry.")
		return
	}

	bySurface := map[string]int{}
	byAction := map[string]int{}
	byRule := map[string]struct{ n, before, after int }{}

	for _, r := range records {
		bySurface[r.Surface]++
		byAction[r.Action]++
		if r.Action == "rewrite" && r.Reason != "" {
			e := byRule[r.Reason]
			e.n++
			e.before += r.BytesBeforeEst
			e.after += r.BytesAfter
			byRule[r.Reason] = e
		}
	}

	fmt.Printf("%s decisions logged %s\n\n", cValue(fmt.Sprintf("%d", len(records))), cDim("("+meta.LogPath()+")"))

	fmt.Println(cHead("By surface"))
	for _, s := range slices.Sorted(maps.Keys(bySurface)) {
		fmt.Printf("  %-24s %d\n", s, bySurface[s])
	}
	fmt.Println()

	fmt.Println(cHead("By action"))
	for _, a := range slices.Sorted(maps.Keys(byAction)) {
		fmt.Printf("  %-24s %d\n", a, byAction[a])
	}
	fmt.Println()

	if len(byRule) > 0 {
		fmt.Println(cHead("Preprocessing rewrites") + cWarn(" (estimated bytes -- per-rule constants, not a measurement of this run)"))
		totalBefore, totalAfter := 0, 0
		for _, rule := range slices.Sorted(maps.Keys(byRule)) {
			e := byRule[rule]
			fmt.Printf("  %-16s %3dx   ~%d -> ~%d bytes\n", rule, e.n, e.before, e.after)
			totalBefore += e.before
			totalAfter += e.after
		}
		fmt.Printf("  %-16s %-6s ~%d -> ~%d bytes %s\n", "total", "", totalBefore, totalAfter, cGood(fmt.Sprintf("(~%d saved)", totalBefore-totalAfter)))
		fmt.Println()
	}

	outcomes, _ := lessons.Scan(meta.OutcomesPath())
	if len(outcomes) > 0 {
		byShape := map[string]struct {
			n      int
			weight float64
		}{}
		for _, o := range outcomes {
			if o.Kind != "escalation" {
				continue
			}
			e := byShape[o.TaskShape]
			e.n++
			e.weight += o.Weight
			byShape[o.TaskShape] = e
		}
		if len(byShape) > 0 {
			fmt.Println(cHead("Escalations") + cDim(" (caller requested a higher tier than the last recommendation for this shape)"))
			shapes := slices.Sorted(maps.Keys(byShape))
			for _, s := range shapes {
				e := byShape[s]
				fmt.Printf("  %-40s %dx (weight %.1f) -- adjusted downshift threshold is now higher for this shape\n", s, e.n, e.weight)
			}
			fmt.Println()
		}
	}

	printUsageCrossCheck()
}

// printUsageCrossCheck reads THIS project's own Claude Code session
// transcripts for real, measured token usage -- the same numbers /usage
// renders -- and prints them next to the estimated figures above. Scope
// mismatch, named rather than hidden: the decision log above is global
// across every project deadeye has ever run in on this machine, while this
// reads only the current working directory's transcripts. Best-effort --
// Claude Code's transcript format and directory layout are undocumented,
// so a miss here just means falling back to the manual /usage check.
func printUsageCrossCheck() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println(cDim("Cross-check these figures against /usage's plugin attribution."))
		return
	}
	t := usage.ScanProject(usage.ConfigDir(), cwd)
	if t.Empty() {
		fmt.Println(cDim("Cross-check these figures against /usage's plugin attribution " +
			"(couldn't find this project's transcripts to cross-check automatically)."))
		return
	}

	fmt.Println(cHead("Real Claude Code usage") + cDim(" (this project's own transcripts, not the global decision log above)"))
	fmt.Printf("  %d sessions scanned · %s total\n\n", t.Sessions, cValue(fmtTokens(t.Total())))

	rows := []struct {
		label, note string
		n           int64
	}{
		{"output", "what Claude wrote back", t.OutputTokens},
		{"input", "fresh context, not from cache -- small is normal", t.InputTokens},
		{"cache read", "reused from cache -- cheap, billed at a discount", t.CacheReadTokens},
		{"cache write", "newly cached for later reuse -- a one-time cost", t.CacheCreationTokens},
	}
	for _, r := range rows {
		fmt.Printf("  %-12s %-12s %s\n", r.label, fmtHuman(r.n)+" tok", cDim(r.note))
	}
	fmt.Println()
	fmt.Println(cDim("Measured by Claude Code itself, not estimated by deadeye. Still worth" +
		" cross-checking against /usage's plugin attribution for the per-plugin split."))
}

// fmtTokens renders n as a token count that reads at a glance -- a rounded
// K/M/B figure first, the exact comma-grouped count in parens after, since
// this is measured data (unlike the estimates above it) and shouldn't hide
// its own precision.
func fmtTokens(n int64) string {
	return fmtHuman(n) + " tokens " + cDim("("+fmtCommaInt64(n)+")")
}

// fmtHuman renders n rounded to the nearest K/M/B, or plain below 1000.
func fmtHuman(n int64) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case abs >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case abs >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// fmtCommaInt64 renders n with thousands separators.
func fmtCommaInt64(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
