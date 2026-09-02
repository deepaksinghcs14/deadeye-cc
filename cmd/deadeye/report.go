package main

import (
	"fmt"
	"os"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
	"github.com/deepaksinghcs14/deadeye-cc/internal/report"
)

// reportRecordKinds are the only Action values `deadeye report record`
// will write -- a fixed, closed vocabulary, same posture as
// recordableKinds in lessons.go. "finding" is the only kind that takes an
// argument (a lens:severity shape, validated by the shapeRe this file
// shares with lessons.go -- same package, same regex, not duplicated).
var reportRecordKinds = map[string]bool{
	"reviewed": true, "finding": true, "posted": true, "skipped": true,
}

const reportRecordUsage = "usage: deadeye report record <reviewed|finding|posted|skipped> [lens:severity]"

// runReport backs `deadeye report` (generate + print the path) and
// `deadeye report record <kind> [arg]` (the /deadeye-pr write-back CLI,
// docs/PRD-lessons.md's PR-review-activity follow-up). Generation is also
// called from runGain so the link `deadeye stats`/`/deadeye-stats` prints
// is never stale (internal/report.Generate is the one shared entry point).
func runReport(args []string) {
	if len(args) > 0 && args[0] == "record" {
		runReportRecord(args[1:])
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye report:", err)
		os.Exit(1)
	}
	path, err := report.Generate(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye report:", err)
		os.Exit(1)
	}
	fmt.Println("file://" + path)
}

// runReportRecord backs `deadeye report record <kind> [lens:severity]`,
// called by /deadeye-pr once per review event (docs/PRD-lessons.md): a
// bare `reviewed` every run regardless of findings, one `finding` per
// finding that survives to the final report, one `posted` per finding
// actually included in a --post'ed review, one `skipped` per finding
// dropped in the existing "Don't repeat" dedup pass. Best-effort by
// contract, same as `deadeye lessons record` -- any failure here is a
// quiet exit, never a reason to withhold the review itself.
//
// Written straight to decisions.jsonl via logstore, not outcomes.jsonl:
// this is raw PR-review ACTIVITY (a count that never decays or biases a
// threshold), not a learning-loop OUTCOME -- see internal/report's package
// doc for why the two stay separate stores.
func runReportRecord(args []string) {
	if len(args) == 0 || !reportRecordKinds[args[0]] {
		fmt.Fprintln(os.Stderr, reportRecordUsage)
		os.Exit(2)
	}
	kind := args[0]
	reason := ""
	if kind == "finding" {
		if len(args) != 2 || !shapeRe.MatchString(args[1]) {
			fmt.Fprintln(os.Stderr, `usage: deadeye report record finding <lens>:<severity>  (e.g. "security:critical")`)
			os.Exit(2)
		}
		reason = args[1]
	} else if len(args) != 1 {
		fmt.Fprintln(os.Stderr, reportRecordUsage)
		os.Exit(2)
	}
	err := logstore.Open(meta.LogPath()).Append(logstore.Record{
		TS: nowRFC3339(), Surface: "PRReview", Action: kind, Reason: reason,
	})
	if err != nil {
		os.Exit(1)
	}
}
