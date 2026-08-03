package main

import (
	"fmt"
	"sort"

	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runAudit backs the /deadeye-audit slash command: everything it reports
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
	for _, s := range sortedKeys(bySurface) {
		fmt.Printf("  %-24s %d\n", s, bySurface[s])
	}
	fmt.Println()

	fmt.Println(cHead("By action"))
	for _, a := range sortedKeys(byAction) {
		fmt.Printf("  %-24s %d\n", a, byAction[a])
	}
	fmt.Println()

	if len(byRule) > 0 {
		fmt.Println(cHead("Preprocessing rewrites") + cWarn(" (estimated bytes -- per-rule constants, not a measurement of this run)"))
		totalBefore, totalAfter := 0, 0
		for _, rule := range sortedRuleKeys(byRule) {
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
			shapes := make([]string, 0, len(byShape))
			for s := range byShape {
				shapes = append(shapes, s)
			}
			sort.Strings(shapes)
			for _, s := range shapes {
				e := byShape[s]
				fmt.Printf("  %-40s %dx (weight %.1f) -- adjusted downshift threshold is now higher for this shape\n", s, e.n, e.weight)
			}
			fmt.Println()
		}
	}

	fmt.Println(cDim("Cross-check these figures against /usage's plugin attribution."))
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedRuleKeys(m map[string]struct{ n, before, after int }) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
