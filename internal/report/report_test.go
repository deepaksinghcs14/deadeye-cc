package report

import (
	"strings"
	"testing"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
)

func testCatalog() catalog.Catalog {
	return catalog.Catalog{Models: []catalog.Model{
		{ID: "haiku-id", Family: "haiku", Tier: 0},
		{ID: "sonnet-id", Family: "sonnet", Tier: 1},
		{ID: "opus-id", Family: "opus", Tier: 2},
	}}
}

// TestBuildNeverBlendsMeasuredAndEstimated is the regression test for the
// one invariant this package inherits from gain.go/audit.go: a "measured"
// byte total and a "rewrite" (estimated) count must never be summed into
// one figure. Build keeps them as separate Kpi entries.
func TestBuildNeverBlendsMeasuredAndEstimated(t *testing.T) {
	logs := []logstore.Record{
		{Action: "measured", Reason: "test-filter", BytesAfter: 500},
		{Action: "rewrite", BytesBeforeEst: 900, BytesAfter: 100},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	var measuredKpi *Kpi
	for i := range d.Kpis {
		if d.Kpis[i].Label == "Bytes filtered (measured)" {
			measuredKpi = &d.Kpis[i]
		}
	}
	if measuredKpi == nil {
		t.Fatal("missing measured Kpi")
	}
	if measuredKpi.Value != "500" {
		t.Errorf("measured Kpi = %q, want \"500\" -- the rewrite's estimated bytes must not be added in", measuredKpi.Value)
	}
}

func TestBuildFamilyBarsFixedOrderNeverByValue(t *testing.T) {
	// opus has the highest count, but the bars must still render
	// sonnet-haiku-opus-fable order -- color/order follows the entity,
	// never the rank (dataviz anti-pattern: recolor/reorder-on-value).
	logs := []logstore.Record{
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "opus-id"},
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "opus-id"},
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "haiku-id"},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	if len(d.RoutingBars) != 4 {
		t.Fatalf("got %d bars, want 4 (fixed sonnet/haiku/opus/fable)", len(d.RoutingBars))
	}
	if d.RoutingBars[0].Label != "Sonnet" || d.RoutingBars[1].Label != "Haiku" || d.RoutingBars[2].Label != "Opus" || d.RoutingBars[3].Label != "Fable" {
		t.Errorf("bar order = %+v, want fixed Sonnet/Haiku/Opus/Fable regardless of counts", d.RoutingBars)
	}
	if d.RoutingBars[2].Percent != 100 {
		t.Errorf("Opus (the max) Percent = %d, want 100", d.RoutingBars[2].Percent)
	}
	if d.RoutingTotal != 3 {
		t.Errorf("RoutingTotal = %d, want 3", d.RoutingTotal)
	}
}

// TestBuildFamilyBarsNilWhenNoRowResolves is the regression test for a
// real bug caught in live verification: an empty (or all-unresolvable)
// log used to render four all-zero bars, hiding the template's honest
// "No routing decisions logged yet" empty state behind a fake-looking
// chart. A routing row from before the Model field existed (or an id the
// loaded catalog doesn't recognize) must not crash or miscount either --
// it's just excluded, same as any other "unknown routes up/gets excluded"
// posture elsewhere -- but when NOTHING resolves, the bars must be nil.
func TestBuildFamilyBarsNilWhenNoRowResolves(t *testing.T) {
	logs := []logstore.Record{
		{Surface: "PreToolUse/Agent", Action: "advise", Model: ""},
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "some-unknown-id"},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	if d.RoutingBars != nil {
		t.Errorf("got %+v, want nil -- neither row resolves to a known family", d.RoutingBars)
	}
}

// TestBuildFamilyBarsExcludesUnknownButKeepsKnown: a mix of resolvable and
// unresolvable rows must still render the resolvable ones correctly,
// silently dropping only the unresolvable one from the count.
func TestBuildFamilyBarsExcludesUnknownButKeepsKnown(t *testing.T) {
	logs := []logstore.Record{
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "haiku-id"},
		{Surface: "PreToolUse/Agent", Action: "advise", Model: "some-unknown-id"},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	if d.RoutingTotal != 1 {
		t.Errorf("RoutingTotal = %d, want 1 -- the unknown-model row must not count", d.RoutingTotal)
	}
}

func TestBuildRuleBarsFromMeasuredAction(t *testing.T) {
	logs := []logstore.Record{
		{Action: "measured", Reason: "test-filter", BytesAfter: 300},
		{Action: "measured", Reason: "test-filter", BytesAfter: 200},
		{Action: "measured", Reason: "install-filter", BytesAfter: 100},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	if len(d.HygieneBars) != 2 {
		t.Fatalf("got %d hygiene bars, want 2 distinct rules", len(d.HygieneBars))
	}
	// sorted alphabetically (slices.Sorted(maps.Keys(...))): install-filter, test-filter
	if d.HygieneBars[1].Label != "test-filter" || d.HygieneBars[1].Value != "500" {
		t.Errorf("test-filter bar = %+v, want combined 500 bytes across its two rows", d.HygieneBars[1])
	}
}

func TestBuildSecurityRowsOnlyWhenNonzero(t *testing.T) {
	d := Build([]logstore.Record{{Action: "exfil-ask"}}, nil, "repo", testCatalog(), time.Now())
	if len(d.SecurityRows) != 1 || d.SecurityRows[0].Value != "1" {
		t.Errorf("got %+v, want exactly one exfil-ask row", d.SecurityRows)
	}
}

func TestBuildSurfaceCardsRoutingEscalation(t *testing.T) {
	outcomes := []lessons.Outcome{
		{Kind: "escalation", TaskShape: "files=1,impl=true,tests=false", Weight: 1.0},
	}
	d := Build(nil, outcomes, "repo", testCatalog(), time.Now())
	routing := d.Surfaces[0]
	if routing.Name != "Routing" || routing.Badge != "1 escalation" {
		t.Errorf("got %+v, want a routing card badged \"1 escalation\"", routing)
	}
	if !strings.Contains(routing.Note, "files=1,impl=true,tests=false") {
		t.Errorf("routing note = %q, want it to name the escalated shape", routing.Note)
	}
}

func TestBuildSurfaceCardsCoderMissesScopedToRepo(t *testing.T) {
	now := time.Now()
	outcomes := []lessons.Outcome{
		{Surface: lessons.SurfaceCoder, Repo: "this-repo", TaskShape: "security:inject", Kind: "coder-miss", Weight: 1, TS: now.Format(time.RFC3339)},
		{Surface: lessons.SurfaceCoder, Repo: "other-repo", TaskShape: "security:secret", Kind: "coder-miss", Weight: 1, TS: now.Format(time.RFC3339)},
	}
	d := Build(nil, outcomes, "this-repo", testCatalog(), now)
	coder := d.Surfaces[1]
	if len(coder.Shapes) != 1 || coder.Shapes[0].Shape != "security:inject" {
		t.Errorf("got %+v, want only this-repo's shape, other-repo's must not leak in", coder)
	}
	if !strings.Contains(coder.Note, "security:inject") {
		t.Errorf("coder note = %q, want it to echo the exact SessionStart reminder text", coder.Note)
	}
}

// TestBuildTrendChartNilWithSparseHistory: a shape that only occurred in
// ONE week isn't a trend -- a one-point line would be misleading, not
// illustrative, so Build must omit the chart entirely rather than fake a
// slope from a single dot.
func TestBuildTrendChartNilWithSparseHistory(t *testing.T) {
	now := time.Now()
	outcomes := []lessons.Outcome{
		{Surface: lessons.SurfaceCoder, Repo: "repo", TaskShape: "security:inject", Kind: "coder-miss", Weight: 1, TS: now.Format(time.RFC3339)},
	}
	d := Build(nil, outcomes, "repo", testCatalog(), now)
	if d.TrendChart != nil {
		t.Errorf("got a trend chart from a single occurrence, want nil")
	}
}

// TestBuildTrendChartDecliningShape is the "how the lessons made it
// wiser" scenario: occurrences across several weeks, declining toward now.
// Asserts the chart exists, points are oldest-to-newest left-to-right, and
// the reminder marker sits at the shape's first-ever occurrence.
func TestBuildTrendChartDecliningShape(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour
	shape := "security:inject"
	outcomes := []lessons.Outcome{
		{Surface: lessons.SurfaceCoder, Repo: "repo", TaskShape: shape, Kind: "coder-miss", Weight: 1, TS: now.Add(-4 * week).Format(time.RFC3339)},
		{Surface: lessons.SurfaceCoder, Repo: "repo", TaskShape: shape, Kind: "coder-miss", Weight: 1, TS: now.Add(-3 * week).Format(time.RFC3339)},
		{Surface: lessons.SurfaceCoder, Repo: "repo", TaskShape: shape, Kind: "coder-miss", Weight: 1, TS: now.Add(-3 * week).Format(time.RFC3339)},
		{Surface: lessons.SurfaceCoder, Repo: "repo", TaskShape: shape, Kind: "coder-miss", Weight: 1, TS: now.Add(-1 * week).Format(time.RFC3339)},
	}
	d := Build(nil, outcomes, "repo", testCatalog(), now)
	if d.TrendChart == nil {
		t.Fatal("got nil chart, want one -- occurrences span 3 distinct weeks")
	}
	c := d.TrendChart
	if c.Shape != shape {
		t.Errorf("chart shape = %q, want %q", c.Shape, shape)
	}
	if len(c.Points) != trendWeeks {
		t.Fatalf("got %d points, want %d", len(c.Points), trendWeeks)
	}
	if c.Points[0].Label == "now" || c.Points[len(c.Points)-1].Label != "now" {
		t.Errorf("points must run oldest-to-newest left-to-right, got first=%q last=%q", c.Points[0].Label, c.Points[len(c.Points)-1].Label)
	}
	if c.Points[0].X >= c.Points[len(c.Points)-1].X {
		t.Errorf("X coordinates must increase left-to-right: first=%v last=%v", c.Points[0].X, c.Points[len(c.Points)-1].X)
	}
	// the 3-weeks-ago bucket has 2 occurrences, the highest -- its Y must
	// be the smallest (SVG y grows downward, so the peak sits highest on
	// screen at the smallest y).
	var peakY, weekAgo1Y float64
	for _, p := range c.Points {
		if p.Label == "3 wks ago" {
			peakY = p.Y
		}
		if p.Label == "1 wks ago" {
			weekAgo1Y = p.Y
		}
	}
	if !(peakY < weekAgo1Y) {
		t.Errorf("peak week (2 occurrences, y=%v) should plot higher (smaller y) than a 1-occurrence week (y=%v)", peakY, weekAgo1Y)
	}
	if !c.HasReminder {
		t.Error("want HasReminder true -- the shape has a first-ever occurrence within the window")
	}
}

func TestBuildPRStatsNilWhenNeverUsed(t *testing.T) {
	d := Build([]logstore.Record{{Surface: "PreToolUse/Bash", Action: "rewrite"}}, nil, "repo", testCatalog(), time.Now())
	if d.PR != nil {
		t.Errorf("got %+v, want nil -- PRReview surface was never logged", d.PR)
	}
}

func TestBuildPRStatsAggregatesFindingsByLensAndSeverity(t *testing.T) {
	logs := []logstore.Record{
		{Surface: "PRReview", Action: "reviewed"},
		{Surface: "PRReview", Action: "reviewed"},
		{Surface: "PRReview", Action: "finding", Reason: "security:critical"},
		{Surface: "PRReview", Action: "finding", Reason: "security:high"},
		{Surface: "PRReview", Action: "finding", Reason: "correctness:medium"},
		{Surface: "PRReview", Action: "posted"},
		{Surface: "PRReview", Action: "skipped"},
	}
	d := Build(logs, nil, "repo", testCatalog(), time.Now())
	if d.PR == nil {
		t.Fatal("got nil PR stats, want non-nil -- PRReview rows were logged")
	}
	if d.PR.Reviewed != 2 || d.PR.Findings != 3 || d.PR.Posted != 1 || d.PR.Skipped != 1 {
		t.Errorf("got %+v, want Reviewed=2 Findings=3 Posted=1 Skipped=1", d.PR)
	}
	if len(d.PR.ByLens) != 2 {
		t.Errorf("got %d lens bars, want 2 (security, correctness)", len(d.PR.ByLens))
	}
	if len(d.PR.BySeverity) != 3 {
		t.Errorf("got %d severity bars, want 3 (critical, high, medium)", len(d.PR.BySeverity))
	}
	// severity order is fixed worst-first, not by count
	if d.PR.BySeverity[0].Label != "Critical" {
		t.Errorf("BySeverity[0] = %q, want \"Critical\" first regardless of count", d.PR.BySeverity[0].Label)
	}
}

func TestRenderEmptyDataDoesNotPanic(t *testing.T) {
	html, err := Render(Data{GeneratedAt: "now", Repo: "repo"})
	if err != nil {
		t.Fatalf("Render errored on empty Data: %v", err)
	}
	if !strings.Contains(html, "repo") {
		t.Error("rendered HTML missing the repo name")
	}
}

func TestRenderIncludesTrendChartWhenPresent(t *testing.T) {
	d := Data{
		GeneratedAt: "now", Repo: "repo",
		TrendChart: &TrendChart{
			Shape:      "security:inject",
			LinePoints: "68,67.5 620,180",
			AreaPath:   "M 68,67.5 L 620,180 Z",
			Points:     []TrendPoint{{Label: "now", Value: 0, X: 620, Y: 180, IsEndpoint: true}},
			MaxLabel:   "4", MidLabel: "2", MidY: 105,
		},
	}
	html, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "security:inject") {
		t.Error("rendered HTML missing the featured shape name")
	}
}
