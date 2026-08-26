package main

import (
	"fmt"
	"sort"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runContext backs `deadeye context` / the /deadeye-stats command: a
// per-session ranked breakdown of context bytes by source, from the
// decision log. You can't trim what you can't see -- this is the
// diagnostic behind every context-hygiene rule.
func runContext(sessionArg string) {
	renderContext(meta.LogPath(), sessionArg)
}

// injectActions are the log actions that record deadeye's OWN injections
// into model context -- all real len() measurements at injection time.
var injectActions = map[string]string{
	"inject":          "session guidance",
	"inject-codemap":  "codebase map",
	"coder-inject":    "coder ruleset",
	"coder-subagent":  "coder subagent card",
	"inject-subagent": "subagent brevity note",
}

func renderContext(logPath, sessionArg string) {
	records, err := logstore.Scan(logPath)
	if err != nil {
		fmt.Println("deadeye context:", err)
		return
	}
	if len(records) == 0 {
		fmt.Println("No decisions logged yet -- run a session with the plugin active, then retry.")
		return
	}

	// Newest TS per session -- RFC3339 UTC sorts lexically, so max(TS)
	// picks the most recent activity.
	latest := map[string]string{}
	for _, r := range records {
		if r.SessionID == "" {
			continue
		}
		if r.TS > latest[r.SessionID] {
			latest[r.SessionID] = r.TS
		}
	}
	if len(latest) == 0 {
		fmt.Println("No session-tagged decisions in the log yet.")
		return
	}
	bySessionTS := make([]string, 0, len(latest))
	for id := range latest {
		bySessionTS = append(bySessionTS, id)
	}
	sort.Slice(bySessionTS, func(i, j int) bool { return latest[bySessionTS[i]] > latest[bySessionTS[j]] })

	session := sessionArg
	if session == "" {
		session = bySessionTS[0]
	} else if _, ok := latest[session]; !ok {
		fmt.Printf("No decisions logged for session %q. Newest sessions:\n", session)
		for i, id := range bySessionTS {
			if i == 5 {
				break
			}
			fmt.Printf("  %s  %s\n", id, cDim(latest[id]))
		}
		return
	}

	type bucket struct {
		count int
		bytes int
	}
	injected := map[string]bucket{} // action -> totals (real bytes)
	arrivals := map[string]bucket{} // reason/tool -> totals (real bytes)
	unsizedSubagent := 0            // pre-0.16 inject-subagent rows carried no size
	var estBefore, estAfter, rewrites int
	var measuredRuns, measuredBytes int

	for _, r := range records {
		if r.SessionID != session {
			continue
		}
		switch {
		case injectActions[r.Action] != "":
			if r.Action == "inject-subagent" && r.BytesAfter == 0 {
				unsizedSubagent++ // never fold an unrecorded size into a total as 0
				continue
			}
			b := injected[r.Action]
			b.count++
			b.bytes += r.BytesAfter
			injected[r.Action] = b
		case r.Action == "observed":
			b := arrivals[r.Reason]
			b.count++
			b.bytes += r.BytesAfter
			arrivals[r.Reason] = b
		case r.Action == "advise" && r.Reason == "large-paste":
			b := arrivals["large-paste (prompt)"]
			b.count++
			b.bytes += r.BytesAfter
			arrivals["large-paste (prompt)"] = b
		case r.Action == "rewrite":
			rewrites++
			estBefore += r.BytesBeforeEst
			estAfter += r.BytesAfter
		case r.Action == "measured":
			measuredRuns++
			measuredBytes += r.BytesAfter
		}
	}

	sortedKeys := func(m map[string]bucket) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if m[keys[i]].bytes != m[keys[j]].bytes {
				return m[keys[i]].bytes > m[keys[j]].bytes
			}
			return keys[i] < keys[j]
		})
		return keys
	}

	fmt.Println("  " + cHead("deadeye context") + cDim(fmt.Sprintf("        session %s (newest of %d; `deadeye context <id>` for another)", session, len(latest))))
	fmt.Println()

	fmt.Println("  " + cHead("Injected by deadeye") + cDim("    measured -- real bytes at injection time"))
	injTotal := 0
	for _, action := range sortedKeys(injected) {
		b := injected[action]
		injTotal += b.bytes
		fmt.Printf("    %-18s %2dx  %8s B   %s\n", action, b.count, fmtBytes(b.bytes), cDim(injectActions[action]))
	}
	if unsizedSubagent > 0 {
		fmt.Printf("    %-18s %2dx  %8s     %s\n", "inject-subagent", unsizedSubagent, "--", cDim("size not recorded (pre-0.16 rows), excluded from total"))
	}
	if injTotal > 0 {
		fmt.Printf("    %-18s      %8s B\n", "total", fmtBytes(injTotal))
	} else if len(injected) == 0 && unsizedSubagent == 0 {
		fmt.Println(cDim("    none this session"))
	}
	fmt.Println()

	fmt.Println("  " + cHead("Observed arrivals") + cDim("      measured sizes; only outliers are logged (built-ins >8KB,"))
	fmt.Println(cDim("                         all MCP responses) -- a floor, not a session total"))
	if len(arrivals) == 0 {
		fmt.Println(cDim("    none logged this session"))
	}
	for _, reason := range sortedKeys(arrivals) {
		b := arrivals[reason]
		fmt.Printf("    %-24s %2dx  %10s B\n", reason, b.count, fmtBytes(b.bytes))
	}
	fmt.Println()

	fmt.Println("  " + cHead("Kept out of context"))
	if rewrites == 0 && measuredRuns == 0 {
		fmt.Println(cDim("    no rewrites this session"))
	}
	if rewrites > 0 {
		fmt.Printf("    %s   ~%s -> ~%s B in %d rewrite%s %s\n", cWarn("estimated"), fmtBytes(estBefore), fmtBytes(estAfter), rewrites, plural(rewrites), cDim("(per-rule typical-case constants)"))
	}
	if measuredRuns > 0 {
		fmt.Printf("    %s    %d run%s, %s B of actual filtered output\n", cGood("measured"), measuredRuns, plural(measuredRuns), cGood(fmtBytes(measuredBytes)))
	}
}
