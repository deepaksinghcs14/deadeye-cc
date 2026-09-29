package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/receipts"
)

// fixShaped matches a commit subject that claims to be repairing
// something. Deliberately narrow: this heuristic decides which commits
// get LOOKED at, never on its own whether a review missed anything --
// that still requires the line overlap below, and even then the finding
// ships as `likely` for a human to confirm.
//
// "prefix"/"suffix" don't match: \b needs a word boundary before "fix".
var fixShaped = regexp.MustCompile(`(?i)^(fix|hotfix|bugfix|revert)\b|\bfix(es|ed)?\b|\bbug\b`)

// maxRangesPerReceipt bounds the git work one receipt can trigger. A
// review of a 40-file diff otherwise fans out to hundreds of `git log -L`
// calls, each its own subprocess -- and the point of this report is a
// signal, not exhaustive coverage. Ranges beyond the cap are counted and
// disclosed rather than silently dropped.
const maxRangesPerReceipt = 20

// gitReader is the one seam this report needs for testing: the logic
// around it is pure, and the fake returns canned output per arg vector.
type gitReader interface {
	run(args ...string) string
}

type realGit struct{ root string }

func (g realGit) run(args ...string) string { return gitutil.Output(g.root, args...) }

type commitInfo struct {
	SHA     string
	Subject string
}

// candidate is one possible review miss: a review passed base..landing
// clean, and a later fix-shaped commit came back to the very lines
// landing introduced.
type candidate struct {
	Receipt receipts.Receipt
	Landing commitInfo
	Fix     commitInfo
	Path    string
	Lines   gitutil.Range
}

// runMisses is repo-scoped by design -- the same scoping `deadeye lessons
// priority` uses. Reviewer accuracy in one codebase says nothing about
// accuracy in another, and the report reads the CURRENT repo's history.
func runMisses() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Println("deadeye misses: no working directory")
		return
	}
	root := gitutil.Root(cwd)
	if root == "" {
		fmt.Println("deadeye misses: not a git repository -- this report reads the repo's own history.")
		return
	}
	renderMisses(meta.ReceiptsPath(), meta.OutcomesPath(), gitutil.ProjectKey(cwd), realGit{root: root})
}

func renderMisses(receiptsPath, outcomesPath, repo string, g gitReader) {
	all, err := receipts.Scan(receiptsPath)
	if err != nil {
		fmt.Println("deadeye misses:", err)
		return
	}
	rs := receipts.ForRepo(all, repo, receipts.KindReview)
	if len(rs) == 0 {
		fmt.Println("No review receipts for " + repo + " yet -- run /deadeye-review, /deadeye-pr or")
		fmt.Println("/deadeye-guard once, then retry. Accuracy is measured going forward from the")
		fmt.Println("first reviewed diff, never backfilled from history.")
		return
	}

	var (
		landed     int
		pending    int
		truncated  int
		candidates []candidate
	)
	for _, r := range rs {
		if r.Commit == "" || len(r.Paths) == 0 {
			// A whole-repo or PR pass with no recorded paths has no single
			// diff to scan forward from. Not a pass, not a miss -- excluded.
			continue
		}
		commits := logAfter(g, r.Commit, r.Paths)
		if len(commits) == 0 {
			pending++
			continue
		}
		landed++
		landing := commits[0]
		ranges := gitutil.AddedRanges(g.run("show", "--unified=0", "--format=", landing.SHA))
		seen := map[string]bool{}
		budget := maxRangesPerReceipt
		for path, rr := range ranges {
			for _, lr := range rr {
				if budget <= 0 {
					truncated++
					continue
				}
				budget--
				for _, c := range logLineRange(g, path, lr, landing.SHA) {
					if seen[c.SHA] || !fixShaped.MatchString(c.Subject) {
						continue
					}
					seen[c.SHA] = true
					candidates = append(candidates, candidate{
						Receipt: r, Landing: landing, Fix: c, Path: path, Lines: lr,
					})
				}
			}
		}
	}

	disputes := disputeCount(outcomesPath, repo)

	fmt.Println("  " + cHead("deadeye misses") + cDim("        did anything come back to fix what a review passed?"))
	fmt.Println()
	fmt.Printf("  %s     %s in %s\n", cHead("Reviews recorded"), cValue(fmt.Sprintf("%d", len(rs))), repo)
	fmt.Printf("  %s        %s reviewed diffs whose work has landed and is scannable\n", cHead("Scannable"), cValue(fmt.Sprintf("%d", landed)))
	if pending > 0 {
		fmt.Printf("  %s          %d reviewed diffs not committed yet %s\n", cHead("Pending"), pending, cDim("(excluded, not counted as passes)"))
	}
	fmt.Println()

	if len(candidates) == 0 {
		if landed == 0 {
			fmt.Println("  " + cDim("Nothing scannable yet -- commit some reviewed work, then retry."))
		} else {
			fmt.Println("  " + cGood("No candidate misses.") + cDim(" No fix-shaped commit has touched the lines"))
			fmt.Println("  " + cDim("any reviewed diff introduced."))
		}
	} else {
		fmt.Printf("  %s  %s %s\n", cHead("Candidate misses"), cWarn(fmt.Sprintf("%d", len(candidates))), cDim("(likely -- confirm each before trusting it)"))
		fmt.Println()
		for _, c := range candidates {
			fmt.Printf("    %s %s\n", cWarn("likely"), cValue(shortSHA(c.Fix.SHA))+" "+gitutil.SanitizeControlBytes(c.Fix.Subject))
			fmt.Printf("      %s %s:%d-%d introduced by %s, reviewed at base %s\n",
				cDim("proof:"), c.Path, c.Lines.Start, c.Lines.End,
				shortSHA(c.Landing.SHA), shortSHA(c.Receipt.Commit))
		}
	}

	fmt.Println()
	if disputes > 0 {
		fmt.Printf("  %s  %d finding(s) you disputed %s\n", cHead("False positives"), disputes, cDim("(from deadeye lessons record review-false-positive)"))
	} else {
		fmt.Println("  " + cHead("False positives") + cDim("  none recorded -- dispute one with"))
		fmt.Println("  " + cDim("                    deadeye lessons record review-false-positive <lens>:<tag>"))
	}
	if truncated > 0 {
		fmt.Printf("  %s        %d line range(s) past the per-review cap of %d went unscanned\n", cHead("Truncated"), truncated, maxRangesPerReceipt)
	}
	fmt.Println()
	fmt.Println(cDim("  Every candidate above is likely, never confirmed. A fix-shaped commit"))
	fmt.Println(cDim("  touching reviewed lines is evidence a review may have missed something --"))
	fmt.Println(cDim("  it is not proof. The fix may be new work, a refactor, or a bug the diff"))
	fmt.Println(cDim("  never contained. Nothing here is recorded automatically; when one is"))
	fmt.Println(cDim("  genuinely a miss, record it yourself so the next review weighs it:"))
	fmt.Println("  " + cValue("deadeye lessons record external-miss <lens>:<tag>"))
}

// logAfter lists commits after base that touched any of paths, oldest
// first. The oldest is the landing commit -- the one that carried the
// reviewed work into history.
func logAfter(g gitReader, base string, paths []string) []commitInfo {
	args := []string{"log", "--format=%H%x00%s", "--reverse", base + "..HEAD", "--"}
	args = append(args, paths...)
	return parseCommits(g.run(args...))
}

// logLineRange lists commits after landing that touched the given line
// range of path. `git log -L` follows the range through later edits, which
// is why this doesn't try to intersect raw hunk offsets itself -- line
// numbers drift as a file changes, and git already solves that.
func logLineRange(g gitReader, path string, r gitutil.Range, landing string) []commitInfo {
	spec := fmt.Sprintf("-L%d,%d:%s", r.Start, r.End, path)
	return parseCommits(g.run("log", "-s", "--format=%H%x00%s", spec, landing+"..HEAD"))
}

func parseCommits(out string) []commitInfo {
	if out == "" {
		return nil
	}
	var cs []commitInfo
	for _, line := range strings.Split(out, "\n") {
		sha, subject, ok := strings.Cut(line, "\x00")
		if !ok || sha == "" {
			continue
		}
		cs = append(cs, commitInfo{SHA: sha, Subject: subject})
	}
	return cs
}

// disputeCount counts review-false-positive outcomes for this repo -- the
// other error direction, already recorded on dispute, shown beside the
// candidate misses so the report never implies only one kind of error
// exists.
func disputeCount(outcomesPath, repo string) int {
	outs, err := lessons.Scan(outcomesPath)
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range outs {
		if o.Repo == repo && o.Kind == "review-false-positive" {
			n++
		}
	}
	return n
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
