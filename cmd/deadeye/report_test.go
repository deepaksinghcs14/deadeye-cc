package main

import (
	"testing"

	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

func TestRunReportRecordFindingWritesLensAndSeverity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runReportRecord([]string{"finding", "security:critical"})

	got, err := logstore.Scan(meta.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Surface != "PRReview" || got[0].Action != "finding" || got[0].Reason != "security:critical" {
		t.Errorf("got %+v, want one PRReview/finding row with Reason \"security:critical\"", got)
	}
}

func TestRunReportRecordReviewedTakesNoArg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runReportRecord([]string{"reviewed"})

	got, err := logstore.Scan(meta.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Action != "reviewed" || got[0].Reason != "" {
		t.Errorf("got %+v, want one bare \"reviewed\" row", got)
	}
}

func TestRunReportRecordMultipleCallsAccumulate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runReportRecord([]string{"reviewed"})
	runReportRecord([]string{"finding", "correctness:medium"})
	runReportRecord([]string{"posted"})
	runReportRecord([]string{"skipped"})

	got, err := logstore.Scan(meta.LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4 -- one per call, all under Surface PRReview", len(got))
	}
	for _, r := range got {
		if r.Surface != "PRReview" {
			t.Errorf("row %+v has Surface %q, want \"PRReview\"", r, r.Surface)
		}
	}
}
