package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runGain backs `deadeye gain` / the /deadeye-gain skill: a compact
// scoreboard of what deadeye has actually done on THIS machine, from the
// decision log. Real `measured` bytes and per-rule estimates are shown
// under different labels -- "measured, not estimated" means never blending
// the two, and never printing a per-repo % for code that was never
// written (the honesty boundary the gain skill carries).
func runGain() {
	renderGain(meta.LogPath())
}

func renderGain(logPath string) {
	records, err := logstore.Scan(logPath)
	if err != nil {
		fmt.Println("deadeye gain:", err)
		return
	}
	if len(records) == 0 {
		fmt.Println("No decisions logged yet -- run a session with the plugin active, then retry.")
		return
	}

	var (
		rewrites      int
		estBefore     int
		estAfter      int
		measuredRuns  int
		measuredBytes int
		advisories    int
		mcpObserved   int
		mcpBytes      int
		coderInjects  int
	)
	perRuleMeasured := map[string][2]int{} // rule -> {runs, bytes}
	for _, r := range records {
		switch r.Action {
		case "rewrite":
			rewrites++
			estBefore += r.BytesBeforeEst
			estAfter += r.BytesAfter
		case "measured":
			measuredRuns++
			measuredBytes += r.BytesAfter
			e := perRuleMeasured[r.Reason]
			e[0]++
			e[1] += r.BytesAfter
			perRuleMeasured[r.Reason] = e
		case "advise":
			advisories++
		case "observed":
			mcpObserved++
			mcpBytes += r.BytesAfter
		case "coder-inject":
			coderInjects++
		}
	}

	fmt.Println("  deadeye gain                    this machine's decision log, nothing invented")
	fmt.Println()
	fmt.Printf("  Rewrites          %d verbose commands trimmed before entering context\n", rewrites)
	if rewrites > 0 {
		fmt.Printf("    estimated       ~%s -> ~%s bytes (per-rule typical-case constants, not runs)\n", fmtBytes(estBefore), fmtBytes(estAfter))
	}
	if measuredRuns > 0 {
		fmt.Printf("    measured        %d real runs, %s bytes of actual filtered output\n", measuredRuns, fmtBytes(measuredBytes))
		rules := make([]string, 0, len(perRuleMeasured))
		for rule := range perRuleMeasured {
			rules = append(rules, rule)
		}
		sort.Strings(rules)
		for _, rule := range rules {
			e := perRuleMeasured[rule]
			fmt.Printf("      %-14s %dx  %s bytes real output\n", rule, e[0], fmtBytes(e[1]))
		}
	}
	fmt.Printf("  Advisories        %d nudges (repeat reads, large files, plan-first, routing)\n", advisories)
	if mcpObserved > 0 {
		fmt.Printf("  MCP observed      %d responses, %s bytes (evidence base, no rules yet)\n", mcpObserved, fmtBytes(mcpBytes))
	}
	if coderInjects > 0 {
		fmt.Printf("  Coder mode        %d session injections\n", coderInjects)
	}
	fmt.Println()
	fmt.Println("  No per-repo savings % is shown: the unbuilt version of your code was")
	fmt.Println("  never written, so there is no baseline to subtract from.")
	fmt.Println("  This repo:  /deadeye-debt  (shortcuts you deferred)")
	fmt.Println("              /deadeye-sweep (what's still cuttable)")
	fmt.Println("  Full detail: /deadeye-audit")
}

func fmtBytes(n int) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
