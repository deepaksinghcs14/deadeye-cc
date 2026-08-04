package main

import (
	"fmt"
	"maps"
	"slices"
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
		bigObserved   int
		bigBytes      int
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
			// Two distinct evidence streams share the action: MCP tool
			// responses, and large built-in-tool responses (Read/Grep/
			// Glob/WebFetch/WebSearch over the observation threshold).
			if strings.HasPrefix(r.Reason, "mcp__") {
				mcpObserved++
				mcpBytes += r.BytesAfter
			} else {
				bigObserved++
				bigBytes += r.BytesAfter
			}
		case "coder-inject":
			coderInjects++
		}
	}

	fmt.Println("  " + cHead("deadeye gain") + cDim("                    this machine's decision log, nothing invented"))
	fmt.Println()
	fmt.Printf("  %s          %s verbose commands trimmed before entering context\n", cHead("Rewrites"), cValue(fmt.Sprintf("%d", rewrites)))
	if rewrites > 0 {
		fmt.Printf("    %s       ~%s -> ~%s bytes %s\n", cWarn("estimated"), fmtBytes(estBefore), fmtBytes(estAfter), cDim("(per-rule typical-case constants, not runs)"))
	}
	if measuredRuns > 0 {
		fmt.Printf("    %s        %d real runs, %s bytes of actual filtered output\n", cGood("measured"), measuredRuns, cGood(fmtBytes(measuredBytes)))
		rules := slices.Sorted(maps.Keys(perRuleMeasured))
		for _, rule := range rules {
			e := perRuleMeasured[rule]
			fmt.Printf("      %-14s %dx  %s bytes real output\n", rule, e[0], fmtBytes(e[1]))
		}
	}
	fmt.Printf("  %s        %s nudges %s\n", cHead("Advisories"), cValue(fmt.Sprintf("%d", advisories)), cDim("(repeat reads/commands, large files, unbounded dumps, routing)"))
	if mcpObserved > 0 {
		fmt.Printf("  %s      %d responses, %s bytes %s\n", cHead("MCP observed"), mcpObserved, fmtBytes(mcpBytes), cDim("(evidence base, no rules yet)"))
	}
	if bigObserved > 0 {
		fmt.Printf("  %s   %d large tool responses, %s bytes %s\n", cHead("Also observed"), bigObserved, fmtBytes(bigBytes), cDim("(Read/Grep/Glob/WebFetch over threshold -- evidence base)"))
	}
	if coderInjects > 0 {
		fmt.Printf("  %s        %s session injections\n", cHead("Coder mode"), cValue(fmt.Sprintf("%d", coderInjects)))
	}
	fmt.Println()
	fmt.Println(cDim("  No per-repo savings % is shown: the unbuilt version of your code was"))
	fmt.Println(cDim("  never written, so there is no baseline to subtract from."))
	fmt.Println("  This repo:  " + cValue("/deadeye-debt") + cDim("  (shortcuts you deferred)"))
	fmt.Println("              " + cValue("/deadeye-sweep") + cDim(" (what's still cuttable)"))
	fmt.Println("  Full detail: " + cValue("/deadeye-audit"))
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
