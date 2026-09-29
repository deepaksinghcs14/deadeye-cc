package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
	"github.com/deepaksinghcs14/deadeye-cc/internal/secscan"
)

// coderSessionWindow bounds how long after a coder receipt a commit is
// still attributed to that session. A receipt records the moment coder
// mode came on; commits carry their own author dates, and these are joined
// by time because nothing in git records which persona was in the editor.
//
// That makes attribution a HEURISTIC, and the report says so. 12h is long
// enough for a real working session and short enough that yesterday's
// session doesn't claim today's commits.
const coderSessionWindow = 12 * time.Hour

type adherenceCommit struct {
	SHA     string
	Subject string
	Level   string
	Files   int
	Adds    int
	Dels    int
	NewDeps []secscan.Dep
}

// runAdherence measures the coder ladder's mechanically checkable rungs
// against the diffs that actually shipped while coder mode was on.
//
// It deliberately does NOT estimate lines not written. `deadeye gain`
// already carries that boundary in code -- the unbuilt version has no
// baseline to subtract from -- and inventing one here would be the same
// dishonesty in a new place. What can be checked after the fact is checked;
// what can't is named.
func runAdherence() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("deadeye adherence: no working directory")
		return
	}
	root := gitutil.Root(cwd)
	if root == "" {
		fmt.Println("deadeye adherence: not a git repository -- this report reads the repo's own commits.")
		return
	}
	renderAdherence(meta.ReceiptsPath(), gitutil.ProjectKey(cwd), realGit{root: root})
}

func renderAdherence(receiptsPath, repo string, g gitReader) {
	all, err := receipts.Scan(receiptsPath)
	if err != nil {
		fmt.Println("deadeye adherence:", err)
		return
	}
	rs := receipts.ForRepo(all, repo, receipts.KindCoder)
	if len(rs) == 0 {
		fmt.Println("No coder-mode sessions recorded for " + repo + " yet -- start a session with")
		fmt.Println("coder mode active, then retry. Adherence is measured going forward, never")
		fmt.Println("backfilled: nothing in git records which persona wrote a commit.")
		return
	}

	commits := attributeCommits(g, rs)
	if len(commits) == 0 {
		fmt.Println("  " + cHead("deadeye adherence") + cDim("     the coder ladder, checked on shipped diffs"))
		fmt.Println()
		fmt.Printf("  %d coder session(s) recorded in %s, but no commits landed inside any\n", len(rs), repo)
		fmt.Println("  session window yet. Commit some work written under coder mode, then retry.")
		return
	}

	var (
		totalDeps int
		fileCts   []int
		addCts    []int
		byLevel   = map[string]int{}
	)
	for _, c := range commits {
		totalDeps += len(c.NewDeps)
		fileCts = append(fileCts, c.Files)
		addCts = append(addCts, c.Adds)
		byLevel[c.Level]++
	}

	fmt.Println("  " + cHead("deadeye adherence") + cDim("     the coder ladder, checked on shipped diffs"))
	fmt.Println()
	fmt.Printf("  %s        %s in %s, across %d recorded session(s)\n",
		cHead("Commits"), cValue(fmt.Sprintf("%d", len(commits))), repo, len(rs))
	levels := make([]string, 0, len(byLevel))
	for l := range byLevel {
		levels = append(levels, l)
	}
	sort.Strings(levels)
	for _, l := range levels {
		fmt.Printf("    %-12s %d\n", l, byLevel[l])
	}
	fmt.Println()

	fmt.Println("  " + cHead("Rung 5") + cDim("  \"a library already does it?\" -- new dependencies added"))
	if totalDeps == 0 {
		fmt.Println("    " + cGood("none") + cDim("  no dependency was added to a manifest under coder mode"))
	} else {
		fmt.Printf("    %s across %d commit(s):\n", cWarn(fmt.Sprintf("%d added", totalDeps)), countWithDeps(commits))
		for _, c := range commits {
			for _, d := range c.NewDeps {
				fmt.Printf("      %s  %s %s %s\n", shortSHA(c.SHA), d.Ecosystem, cValue(d.Name), d.Version)
			}
		}
	}
	fmt.Println()

	fmt.Println("  " + cHead("Rung 7") + cDim("  \"the minimum code that works, in the fewest files\""))
	fmt.Printf("    median  %s file(s), %s insertion(s) per commit\n",
		cValue(fmt.Sprintf("%d", median(fileCts))), cValue(fmt.Sprintf("%d", median(addCts))))
	if wide := widestCommits(commits, 3); len(wide) > 0 {
		fmt.Println("    " + cDim("widest by file count:"))
		for _, c := range wide {
			fmt.Printf("      %s  %d files, +%d/-%d  %s\n",
				shortSHA(c.SHA), c.Files, c.Adds, c.Dels, gitutil.SanitizeControlBytes(c.Subject))
		}
	}
	fmt.Println()
	fmt.Println(cDim("  These are signals, not verdicts. A wide commit can be the right shape"))
	fmt.Println(cDim("  (a rename, a generated file) and a new dependency can be the lean call"))
	fmt.Println(cDim("  the ladder's own rung 5 asks for. Read them as a trend, not a score."))
	fmt.Println()
	fmt.Println(cDim("  Not measured, deliberately:"))
	fmt.Println(cDim("    rung 2 (already in this codebase) -- needs a function-level symbol"))
	fmt.Println(cDim("      index this plugin doesn't build; see /deadeye-review's over-engineering"))
	fmt.Println(cDim("      lens, which judges duplication on a diff it can actually read."))
	fmt.Println(cDim("    rungs 1, 3, 4, 6 -- judgment calls, not mechanically checkable."))
	fmt.Println(cDim("    lines NOT written -- no baseline exists; same boundary deadeye gain keeps."))
	fmt.Println(cDim("  Attribution is by time window (12h after a recorded session), not proof:"))
	fmt.Println(cDim("  git records no persona, so a commit written with coder off inside that"))
	fmt.Println(cDim("  window still counts here."))
}

// attributeCommits joins coder receipts to commits by author time. A
// commit reachable from a receipt's base commit and authored inside that
// receipt's window is attributed to it; the first receipt to claim a
// commit keeps it, so overlapping sessions never double-count.
func attributeCommits(g gitReader, rs []receipts.Receipt) []adherenceCommit {
	seen := map[string]bool{}
	var out []adherenceCommit
	for _, r := range rs {
		if r.Commit == "" {
			continue
		}
		start, err := time.Parse(time.RFC3339, r.TS)
		if err != nil {
			continue
		}
		end := start.Add(coderSessionWindow)
		raw := g.run("log", "--format=%H%x00%at%x00%s", r.Commit+"..HEAD")
		for _, line := range strings.Split(raw, "\n") {
			parts := strings.Split(line, "\x00")
			if len(parts) < 3 || parts[0] == "" || seen[parts[0]] {
				continue
			}
			epoch, cerr := strconv.ParseInt(parts[1], 10, 64)
			if cerr != nil {
				continue
			}
			at := time.Unix(epoch, 0)
			if at.Before(start) || at.After(end) {
				continue
			}
			seen[parts[0]] = true
			c := adherenceCommit{SHA: parts[0], Subject: parts[2], Level: r.Level}
			c.Files, c.Adds, c.Dels = numstatVia(g, parts[0])
			c.NewDeps = newDepsIn(g, parts[0])
			out = append(out, c)
		}
	}
	return out
}

// numstatVia mirrors gitutil.Numstat through the gitReader seam so the
// report stays testable without a real repo.
func numstatVia(g gitReader, rev string) (files, adds, dels int) {
	out := g.run("show", "--numstat", "--format=", rev)
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		files++
		if n, err := strconv.Atoi(f[0]); err == nil {
			adds += n
		}
		if n, err := strconv.Atoi(f[1]); err == nil {
			dels += n
		}
	}
	return files, adds, dels
}

// newDepsIn finds dependencies ADDED by a commit, reusing the same
// manifest extractors /deadeye-guard's dependency pass already uses rather
// than re-parsing manifests here.
func newDepsIn(g gitReader, rev string) []secscan.Dep {
	names := g.run("show", "--name-only", "--format=", rev)
	var deps []secscan.Dep
	for _, path := range strings.Split(names, "\n") {
		path = strings.TrimSpace(path)
		if path == "" || !secscan.IsManifest(path) {
			continue
		}
		patch := g.run("show", "--unified=0", "--format=", rev, "--", path)
		deps = append(deps, secscan.ExtractDeps(path, addedLines(patch))...)
	}
	return deps
}

// addedLines pulls just the added content out of a unified diff -- the
// "+" lines with their marker stripped, never the "+++" file header.
func addedLines(patch string) string {
	var b strings.Builder
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			b.WriteString(line[1:])
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func countWithDeps(cs []adherenceCommit) int {
	n := 0
	for _, c := range cs {
		if len(c.NewDeps) > 0 {
			n++
		}
	}
	return n
}

// widestCommits returns up to n commits with the most files touched, only
// when there is a real spread -- a list of three 1-file commits labeled
// "widest" is noise, not a signal.
func widestCommits(cs []adherenceCommit, n int) []adherenceCommit {
	sorted := make([]adherenceCommit, len(cs))
	copy(sorted, cs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Files > sorted[j].Files })
	if len(sorted) == 0 || sorted[0].Files <= median(fileCounts(cs)) {
		return nil
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

func fileCounts(cs []adherenceCommit) []int {
	out := make([]int, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Files)
	}
	return out
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	s := make([]int, len(xs))
	copy(s, xs)
	sort.Ints(s)
	return s[len(s)/2]
}
