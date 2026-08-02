package main

import (
	"fmt"
	"sort"

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

	fmt.Printf("%d decisions logged (%s)\n\n", len(records), meta.LogPath())

	fmt.Println("By surface:")
	for _, s := range sortedKeys(bySurface) {
		fmt.Printf("  %-24s %d\n", s, bySurface[s])
	}
	fmt.Println()

	fmt.Println("By action:")
	for _, a := range sortedKeys(byAction) {
		fmt.Printf("  %-24s %d\n", a, byAction[a])
	}
	fmt.Println()

	if len(byRule) > 0 {
		fmt.Println("Preprocessing rewrites (estimated bytes -- see each rule's EstBeforeBytes/EstAfterBytes, not a measurement of this run):")
		totalBefore, totalAfter := 0, 0
		for _, rule := range sortedRuleKeys(byRule) {
			e := byRule[rule]
			fmt.Printf("  %-16s %3dx   ~%d -> ~%d bytes\n", rule, e.n, e.before, e.after)
			totalBefore += e.before
			totalAfter += e.after
		}
		fmt.Printf("  %-16s %-6s ~%d -> ~%d bytes (~%d saved)\n", "total", "", totalBefore, totalAfter, totalBefore-totalAfter)
		fmt.Println()
	}

	fmt.Println("Cross-check these figures against /usage's plugin attribution.")
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
