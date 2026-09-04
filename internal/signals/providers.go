package signals

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PromptShape is the weakest signal (PLAN.md §3.1: "cap its weight"):
// word count, question density, and complexity/vagueness keyword
// matching. No syntax tree, no model call -- just cheap heuristics.
type PromptShape struct{}

func (PromptShape) Name() string { return "promptshape" }

// strongComplexityWords are structural/scope claims that are hard to
// verify from text alone -- a prompt saying "architecture" or "across the
// codebase" is very likely genuinely broad, and a text claim like that
// can't be sized precisely either way. Firing lowers confidence as well
// as raising complexity, the same treatment as vagueWords below: both are
// "this estimate is a guess."
var strongComplexityWords = []string{
	"architecture", "redesign", "tradeoff", "trade-off", "scalability",
	"concurrency", "race condition", "across the codebase", "every file", "all files",
}

// mildComplexityWords are ordinary engineering verbs used just as often
// for a one-line change ("refactor this function", "migrate this
// constant") as for a genuine rewrite -- recalibrated after real
// production data (172 live routing decisions on this repo) showed a
// single mention of one of these words used to crater confidence to the
// same floor as an actually vague prompt, which blocked downshift for
// nearly anything that used ordinary refactoring vocabulary: only 1 of
// the 172 ever downshifted. These still nudge the complexity score up,
// but -- unlike strongComplexityWords and vagueWords -- don't alone force
// low confidence; the other signals (file scope, task specificity, test
// presence) get to decide whether THIS refactor is actually small.
var mildComplexityWords = []string{
	"refactor", "migrate", "migration", "rewrite",
}

var vagueWords = []string{
	"somehow", "maybe", "not sure", "figure out", "look into", "investigate",
}

func (PromptShape) Assess(_ context.Context, s Scope) (Evidence, error) {
	prompt := strings.ToLower(strings.TrimSpace(s.Prompt))
	if prompt == "" {
		return Evidence{}, fmt.Errorf("promptshape: no prompt to assess")
	}

	// fuzzyMatch tracks whether a STRONG complexity or vague word fired --
	// that's the genuinely uncertain part (translating scattered keyword
	// hits into a numeric magnitude is a guess). mildComplexityWords
	// deliberately does NOT set it: an ordinary verb like "refactor" is
	// common evidence for the complexity score, not evidence the estimate
	// itself is unreliable. Question-mark count and word count are
	// objective, exactly countable facts, not guesses, so they contribute
	// to the complexity score without affecting confidence either.
	score := 0.0
	fuzzyMatch := false
	for _, w := range strongComplexityWords {
		if strings.Contains(prompt, w) {
			score += 0.15
			fuzzyMatch = true
		}
	}
	for _, w := range mildComplexityWords {
		if strings.Contains(prompt, w) {
			score += 0.1
		}
	}
	for _, w := range vagueWords {
		if strings.Contains(prompt, w) {
			score += 0.1
			fuzzyMatch = true
		}
	}
	score += 0.1 * float64(strings.Count(prompt, "?"))
	wordCount := len(strings.Fields(prompt))
	if wordCount > 60 {
		score += 0.15
	}
	if score > 1 {
		score = 1
	}

	// Confidence is about trust in THIS estimate, not in the provider
	// overall: no strong/vague keyword match is a fairly reliable negative
	// result (0.85); once one fires, confidence drops to 0.35, capped as
	// the plan's weakest signal. A mildComplexityWords-only match (a plain
	// "refactor"/"migrate"/"rewrite", nothing structural or vague) stays
	// at 0.85 -- it still raises the complexity score above, but doesn't
	// alone veto downshift; see mildComplexityWords' doc comment. A high
	// complexity reading still forces the ceiling via kernel.Decide's
	// free-upshift path regardless of confidence, so the cap costs nothing
	// there.
	//
	// This split matters and had two real bugs on the way to it: a flat
	// low confidence made downshifting UNREACHABLE for every real prompt
	// (any nonempty prompt participates, and kernel.Decide requires the
	// MINIMUM confidence across all evidence to clear downshift_threshold).
	// The first fix (confidence keyed to score>0) was still broken --
	// gating on the WHOLE score let one stray "?" (extremely common in
	// ordinary task descriptions) drop confidence to 0.35 with no genuine
	// complexity signal at all. Gating on fuzzyMatch specifically (word
	// hits only) fixes both: caught by reproducing Phase 6's escalation
	// detection live and getting a ceiling decision from a plain,
	// question-mark-terminated "what is 2+2?" delegation that should have
	// downshifted.
	confidence := 0.85
	if fuzzyMatch {
		confidence = 0.35
	}
	return Evidence{
		Provider:   "promptshape",
		Complexity: score,
		Confidence: confidence,
		Facts:      map[string]any{"word_count": wordCount},
	}, nil
}

// FileScope reads task complexity off how many files are in scope --
// single-file vs multi-file is one of the cheapest, most reliable signals.
type FileScope struct{}

func (FileScope) Name() string { return "filescope" }

func (FileScope) Assess(_ context.Context, s Scope) (Evidence, error) {
	n := len(s.Files)
	if n == 0 {
		return Evidence{}, fmt.Errorf("filescope: no files in scope")
	}
	var complexity float64
	switch {
	case n == 1:
		complexity = 0.15
	case n <= 3:
		complexity = 0.4
	case n <= 8:
		complexity = 0.65
	default:
		complexity = 0.85
	}
	return Evidence{
		Provider:   "filescope",
		Complexity: complexity,
		Confidence: 0.85, // a file count is a fact, not a guess
		Facts:      map[string]any{"file_count": n},
	}, nil
}

// GitChurn counts recent commits touching the files in scope -- a proxy
// for how volatile/risky the area currently is.
type GitChurn struct{}

func (GitChurn) Name() string { return "gitchurn" }

func (GitChurn) Assess(ctx context.Context, s Scope) (Evidence, error) {
	if s.Repo == "" || len(s.Files) == 0 {
		return Evidence{}, fmt.Errorf("gitchurn: no repo/files to check")
	}

	// An untracked file has no commit history for `git log` to find --
	// that's UNKNOWN, not "calm". Without this check, a brand-new file
	// (arguably the highest-churn case there is) silently read as "0
	// commits, confidence 0.82": the same root cause as the subdirectory
	// bug below -- git doesn't error on a pathspec that matches nothing,
	// so a genuine "nothing to report" and "can't tell" were
	// indistinguishable. `git ls-files` (no --error-unmatch) prints only
	// whichever of the given paths ARE tracked, exiting 0 either way, so
	// empty output here means none of them are.
	lsFiles := exec.CommandContext(ctx, "git", append([]string{"ls-files", "--"}, s.Files...)...)
	lsFiles.Dir = s.Repo
	trackedOut, err := lsFiles.Output()
	if err != nil || strings.TrimSpace(string(trackedOut)) == "" {
		return Evidence{}, fmt.Errorf("gitchurn: no scoped file is tracked")
	}

	// s.Repo must be the repo TOPLEVEL, not an arbitrary subdirectory --
	// git prints scoped-file paths (from `git diff --name-only`) relative
	// to the root, so running `git log -- <root-relative-path>` with
	// cmd.Dir set to a subdirectory matches nothing and, per the same
	// silent-empty-match behavior noted above, was laundered into "0
	// commits" rather than surfaced as wrong scope. newScope (route.go)
	// is what actually resolves the toplevel now; this provider just
	// documents the assumption it depends on.
	args := append([]string{"log", "--since=30.days", "--oneline", "--"}, s.Files...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.Repo
	out, err := cmd.Output()
	if err != nil {
		return Evidence{}, err
	}
	trimmed := strings.TrimSpace(string(out))
	commits := 0
	if trimmed != "" {
		commits = len(strings.Split(trimmed, "\n"))
	}
	var complexity float64
	switch {
	case commits == 0:
		complexity = 0.1
	case commits <= 3:
		complexity = 0.3
	case commits <= 10:
		complexity = 0.55
	default:
		complexity = 0.8
	}
	return Evidence{
		Provider:   "gitchurn",
		Complexity: complexity,
		Confidence: 0.82, // a commit count is a fact, same footing as the other measured (non-heuristic) signals
		Facts:      map[string]any{"commits_last_30d": commits},
	}, nil
}

// TestPresence checks whether files in scope have an adjacent test file.
// Presence lowers the complexity contribution (a safety net exists);
// absence raises it.
type TestPresence struct{}

func (TestPresence) Name() string { return "testpresence" }

func (TestPresence) Assess(_ context.Context, s Scope) (Evidence, error) {
	if len(s.Files) == 0 {
		return Evidence{}, fmt.Errorf("testpresence: no files to check")
	}
	covered := 0
	for _, f := range s.Files {
		if hasAdjacentTest(s.Repo, f) {
			covered++
		}
	}
	ratio := float64(covered) / float64(len(s.Files))
	complexity := 0.6 - 0.5*ratio // 0.1 fully covered .. 0.6 uncovered
	return Evidence{
		Provider:   "testpresence",
		Complexity: complexity,
		Confidence: 0.8, // file existence is a fact; ratio-to-complexity mapping is the only guess
		Facts:      map[string]any{"files_with_adjacent_test": covered, "files_checked": len(s.Files)},
	}, nil
}

// hasAdjacentTest resolves a relative path against repo before stat'ing
// it -- the daemon that calls this may have been spawned from an entirely
// different project's directory, so a bare os.Stat on a relative path
// previously resolved against wherever the daemon happened to start,
// silently checking the wrong project's filesystem. An already-absolute
// path (as some callers -- notably tests -- pass) is used as-is.
func hasAdjacentTest(repo, path string) bool {
	if !filepath.IsAbs(path) && repo != "" {
		path = filepath.Join(repo, path)
	}
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	candidates := []string{
		filepath.Join(dir, stem+"_test"+ext),
		filepath.Join(dir, stem+".test"+ext),
		filepath.Join(dir, "test_"+stem+ext),
		filepath.Join(dir, stem+"_spec"+ext),
		// Python's separate tests/ directory, same-level and one level up
		// (covers both pkg/tests/test_x.py and tests/test_x.py next to pkg/).
		filepath.Join(dir, "tests", "test_"+stem+ext),
		filepath.Join(filepath.Dir(dir), "tests", "test_"+stem+ext),
		// JS/TS's __tests__ directory convention.
		filepath.Join(dir, "__tests__", stem+".test"+ext),
		filepath.Join(dir, "__tests__", stem+".spec"+ext),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

// TaskSpecificity checks whether the PROMPT ITSELF references something
// real, not just how the ambient repo looks -- the gap the other three
// providers leave. filescope/gitchurn/testpresence all read repo state
// (files already touched, recent churn, adjacent tests); none of them
// notice whether the delegated task actually names a file. Verified live:
// "fix it" and a fully file-and-line-anchored bug report routed identically
// against the same quiet, tested, low-churn file -- nothing was measuring
// whether the CALLER did the work of scoping the task. This closes that gap
// by contributing CONFIDENCE, not complexity: a vague delegation shouldn't
// be trusted with a downshift regardless of how safe the repo looks, and
// kernel.Decide's existing min-confidence gate (any single low-confidence
// item blocks downshift) already enforces that with no kernel change needed.
type TaskSpecificity struct{}

func (TaskSpecificity) Name() string { return "taskspecificity" }

// pathToken matches a filename or path shape, with an optional captured
// ":<line>" anchor, with surrounding sentence punctuation stripped by the
// caller before matching -- e.g. "auth/login.go:42" or "login.go".
var pathToken = regexp.MustCompile(`^([\w][\w/-]*\.[A-Za-z0-9]{1,8})(:\d+)?$`)

// backtickIdent matches a code identifier cited the way this codebase's own
// rubrics cite one (e.g. the review rubric's “ `race:` “ tags) -- used to
// verify a prompt names something that actually exists IN the cited file,
// not just a file that exists somewhere in the repo.
var backtickIdent = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*)`")

// proseExt is deliberately small: files whose CONTENT is prose about the
// code rather than code itself. Citing one of these is a real, common
// pattern for genuine context ("per the design doc...") and an equally
// common gaming pattern (naming any real file to borrow its trust for an
// unrelated change) -- the two are indistinguishable without a stronger
// check. Citing a source file by name needs no such check: "Rename x to
// count in a.go" is already specific.
var proseExt = map[string]bool{
	".md": true, ".markdown": true, ".txt": true,
	".rst": true, ".adoc": true, ".rtf": true,
}

// vaguePhrases is TaskSpecificity's own short, fixed list -- mirrors
// PromptShape's vagueWords in spirit but targets whole delegation-shaped
// phrases ("fix it") rather than single words, since a single vague WORD
// inside an otherwise file-anchored task shouldn't retroactively erase the
// anchor.
var vaguePhrases = []string{
	"fix it", "handle it", "handle this", "fix this",
	"make it work", "sort it out", "take care of it",
}

func (TaskSpecificity) Assess(ctx context.Context, s Scope) (Evidence, error) {
	prompt := strings.TrimSpace(s.Prompt)
	if prompt == "" {
		return Evidence{}, fmt.Errorf("taskspecificity: no prompt to assess")
	}

	tracked := trackedFileSet(ctx, s.Repo)
	var idents []string
	for _, m := range backtickIdent.FindAllStringSubmatch(prompt, -1) {
		idents = append(idents, m[1])
	}

	// A REAL file citation isn't enough on its own for a PROSE file --
	// verified live: naming a real tracked README.md, cited purely for
	// context, earned full trust for a change that actually touched a
	// completely different, security-sensitive source file. A source-code
	// citation (any extension not in proseExt) keeps the simple original
	// bar -- "Rename x to count in a.go" is genuinely specific without a
	// line number, and demanding one for every citation broke that real,
	// common case. A prose-file citation additionally requires either a
	// line number (someone actually looked at the file) or a backtick-
	// quoted identifier verified to exist in that file's own content --
	// weakAnchor (a real prose file, cited with neither) is real but
	// unverified relevance, capped at the same trust as no anchor at all.
	strongAnchor, weakAnchor := false, false
	for _, tok := range strings.Fields(prompt) {
		tok = strings.Trim(tok, `,;:!?)("'`+"`")
		m := pathToken.FindStringSubmatch(tok)
		if m == nil {
			continue
		}
		path, hasLine := m[1], m[2] != ""
		if !trackedFileMatches(tracked, path) {
			continue
		}
		weakAnchor = true
		if !proseExt[strings.ToLower(filepath.Ext(path))] || hasLine || fileContainsAny(s.Repo, path, idents) {
			strongAnchor = true
		}
	}

	wordCount := len(strings.Fields(prompt))
	lower := strings.ToLower(prompt)
	vague := false
	for _, phrase := range vaguePhrases {
		if strings.Contains(lower, phrase) {
			vague = true
			break
		}
	}

	var confidence float64
	switch {
	case strongAnchor:
		// A verified, existing repo file, cited with a line number or a
		// symbol confirmed to actually live in that file -- a fact, same
		// footing as filescope/gitchurn's own file-count/commit-count
		// facts, not a guess.
		confidence = 0.85
	case vague || wordCount < 6:
		// No verified-relevant anchor, and either an explicit vague phrase
		// or too short to plausibly contain one -- "fix it" is 2 words.
		// The weakest reading, deliberately capped low so it reliably
		// blocks downshift on its own, the same way PromptShape caps a
		// fuzzy keyword match at 0.35.
		confidence = 0.2
	default:
		// Real-but-unverified (weakAnchor: a real file named with no line
		// or matching symbol -- could be relevant, could just be
		// context-dropping), no anchor shape at all, or a fake/misspelled
		// path. Distinctly less trusted than a verified anchor, but not
		// punished as hard as genuine vagueness -- a legitimate "create a
		// new X" task can't cite a file that doesn't exist yet, and
		// shouldn't be treated the same as "fix it".
		confidence = 0.5
	}

	return Evidence{
		Provider:   "taskspecificity",
		Complexity: 0, // this signal has no opinion on difficulty, only on how much to trust delegating it cheaply
		Confidence: confidence,
		Facts:      map[string]any{"word_count": wordCount, "strong_anchor": strongAnchor, "weak_anchor": weakAnchor},
	}, nil
}

// fileContainsAny reports whether repo-resolved path's content contains any
// of idents as a literal substring -- a cheap, bounded, no-LLM way to
// confirm a backtick-quoted identifier in the PROMPT actually appears in
// the FILE it's cited alongside, not just anywhere in the repo. Any read
// failure (path traversal outside repo, huge/binary file the OS can't open
// cheaply, race with a concurrent delete) degrades to "can't verify" rather
// than blocking the whole assessment.
func fileContainsAny(repo, path string, idents []string) bool {
	if len(idents) == 0 {
		return false
	}
	full := path
	if !filepath.IsAbs(full) && repo != "" {
		full = filepath.Join(repo, full)
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	for _, id := range idents {
		if bytes.Contains(content, []byte(id)) {
			return true
		}
	}
	return false
}

// trackedFileSet lists every file git tracks in repo, bounded by ctx like
// every other git call site. Empty repo, a non-git directory, or any git
// error all degrade to an empty set -- TaskSpecificity still scores on
// wording alone, it just can't verify a path shape as real.
func trackedFileSet(ctx context.Context, repo string) map[string]bool {
	set := map[string]bool{}
	if repo == "" {
		return set
	}
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return set
	}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			set[f] = true
		}
	}
	return set
}

// trackedFileMatches reports whether path is a real tracked file: an exact
// match, or -- for a bare filename with no directory component -- the
// unique tracked file with that basename. A basename that matches more than
// one tracked file is ambiguous and does not count as anchored; the caller
// would need to disambiguate with a directory the way a real bug report
// naturally would.
func trackedFileMatches(tracked map[string]bool, path string) bool {
	if tracked[path] {
		return true
	}
	if strings.Contains(path, "/") {
		return false
	}
	matches := 0
	for f := range tracked {
		if filepath.Base(f) == path {
			matches++
			if matches > 1 {
				return false
			}
		}
	}
	return matches == 1
}

// SubagentKind reads the one piece of information the Agent tool call
// itself carries about the subtask's risk that nothing else here looks at:
// its subagent_type. Only "Explore" is recognized -- Claude Code's built-in
// read-only search agent (no Edit/Write tool access), a genuinely verified
// fact about what it CAN do, not a guess about what it's likely to do.
// Every other value, including "general-purpose" and any custom
// user-defined agent name, carries no information deadeye can verify, so it
// contributes nothing -- same honest "skip when we can't actually assess"
// contract every other provider here follows, rather than pattern-matching
// on arbitrary type names.
type SubagentKind struct{}

func (SubagentKind) Name() string { return "subagentkind" }

// QuietSkip marks this as a bonus signal (see signals.quietSkipper): not
// being "Explore" is the normal case for most subagent calls, not a
// genuine information gap, so its absence must not count toward
// AssessAll's skip penalty.
func (SubagentKind) QuietSkip() bool { return true }

func (SubagentKind) Assess(_ context.Context, s Scope) (Evidence, error) {
	if s.SubagentType != "Explore" {
		return Evidence{}, fmt.Errorf("subagentkind: %q carries no verified risk information", s.SubagentType)
	}
	return Evidence{
		Provider:   "subagentkind",
		Complexity: 0.1, // read-only: it can misreport, but it can't break code
		Confidence: 0.85,
		Facts:      map[string]any{"subagent_type": s.SubagentType},
	}, nil
}
