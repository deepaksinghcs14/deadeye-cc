// Package report renders deadeye's decision log and outcomes store into a
// single self-contained HTML status page (docs/PRD-lessons.md's follow-up:
// "make deadeye better over time" needs a way to actually SEE that it's
// happening). Every figure traces to a real logged row -- the same honesty
// boundaries `deadeye gain`/`deadeye audit` already enforce (measured vs.
// estimated never blended, no invented per-repo percentage) apply here too,
// since this package computes its own aggregates rather than importing
// those commands' unexported ones (cmd/deadeye is a leaf, not a library;
// internal packages don't import from it).
package report

import (
	_ "embed"
	"fmt"
	"html/template"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/gitutil"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

//go:embed report.html.tmpl
var tmplSrc string

// Kpi is one headline number. Value and Sub are pre-formatted -- the
// template does no arithmetic (internal/report's whole job is doing the
// math once, in Go, not in template syntax).
type Kpi struct {
	Label, Value, Sub string
}

// Bar is one row of a magnitude/categorical bar list, already scaled to its
// group's max (Percent 0-100) so the template only sets a CSS width.
type Bar struct {
	Label, Value, Color string
	Percent             int
}

// ShapeCount is one lens:tag shape's recent occurrence count, for a
// Learning Loop surface card -- the same RankedShape data RecentShapes
// already returns, just labeled for direct template use.
type ShapeCount struct {
	Shape string
	Count int
}

// SurfaceCard is one of the three Learning Loop panels (routing/coder/
// pr-review), mirroring the structure `deadeye lessons priority` and the
// SessionStart misses reminder already read.
type SurfaceCard struct {
	Name, Badge, Note string
	Shapes            []ShapeCount
}

// TrendPoint is one plotted week, with its SVG coordinates precomputed --
// see buildTrendChart's viewBox comment for the scale.
type TrendPoint struct {
	Label      string
	Value      int
	X, Y       float64
	IsEndpoint bool
}

// TrendChart is the "how the lessons made it wiser" line chart for the
// single highest-ranked coder-surface shape in this repo. Nil when there
// isn't enough history yet (fewer than two distinct weeks of data) -- a
// one-point "trend" would be misleading, not illustrative.
type TrendChart struct {
	Shape       string
	Points      []TrendPoint
	AreaPath    string
	LinePoints  string
	ReminderX   float64
	HasReminder bool
	MaxLabel    string
	MidLabel    string
	MidY        float64
}

// PRStats is /deadeye-pr's activity, from Surface=="PRReview" rows written
// by `deadeye report record ...`. Nil until that surface has ever logged
// anything -- a repo that has never run /deadeye-pr gets no section at all,
// not a row of zeros.
type PRStats struct {
	Reviewed, Findings, Posted, Skipped int
	ByLens, BySeverity                  []Bar
}

// Data is the fully-resolved view model report.html.tmpl renders. Every
// field is already computed -- no template-side math, per the package doc.
type Data struct {
	GeneratedAt  string
	Repo         string
	Kpis         []Kpi
	TrendChart   *TrendChart
	Surfaces     []SurfaceCard
	RoutingBars  []Bar
	RoutingTotal int
	HygieneBars  []Bar
	SecurityRows []Kpi
	PR           *PRStats
}

// trendWeeks is how far back the featured shape's weekly trend looks --
// long enough to show a decline, short enough to stay one screen.
const trendWeeks = 5

// featuredShapeCount caps how many "recent misses" shapes render per
// surface card -- same bound as the SessionStart reminder and
// `deadeye lessons priority` already use (RecentShapes' own cap), so the
// report never shows more than a session would.
const featuredShapeCount = 3

// categoricalColors are the four validated routing-tier slots (see the
// approved Artifact mock's palette validation) in a fixed, never-cycled
// order: sonnet, haiku, opus, fable -- CSS custom properties the template
// resolves per theme.
var categoricalColors = []string{"cat-1", "cat-2", "cat-3", "cat-4"}

// Generate scans the decision log and outcomes store, builds the view
// model, renders it, and writes ~/.deadeye/report.html, returning its
// absolute path. The single entry point both `deadeye report` and
// `deadeye gain`'s trailing link call -- see cmd/deadeye/report.go and
// cmd/deadeye/gain.go.
func Generate(cwd string) (string, error) {
	logs, err := logstore.Scan(meta.LogPath())
	if err != nil {
		return "", err
	}
	outcomes, err := lessons.Scan(meta.OutcomesPath())
	if err != nil {
		return "", err
	}
	repo := gitutil.ProjectKey(cwd)
	data := Build(logs, outcomes, repo, catalog.Load(), time.Now())
	html, err := Render(data)
	if err != nil {
		return "", err
	}
	path := meta.ReportPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Build computes the view model from already-scanned data -- pure and
// deterministic (now is a parameter, not read internally) so it's testable
// without touching disk, same discipline internal/lessons' own
// AdjustedDownshiftThreshold uses.
func Build(logs []logstore.Record, outcomes []lessons.Outcome, repo string, cat catalog.Catalog, now time.Time) Data {
	d := Data{
		GeneratedAt: now.UTC().Format("2006-01-02 15:04 UTC"),
		Repo:        repo,
	}

	var (
		measuredBytes   int
		rewrites        int
		routingCount    int
		familyCounts    = map[string]int{}
		perRuleMeasured = map[string][2]int{} // rule -> {runs, bytes}
		exfilAsk        int
		vulnAsk         int
		coderInjects    int
	)
	for _, r := range logs {
		switch r.Action {
		case "measured":
			measuredBytes += r.BytesAfter
			e := perRuleMeasured[r.Reason]
			e[0]++
			e[1] += r.BytesAfter
			perRuleMeasured[r.Reason] = e
		case "rewrite":
			rewrites++
		case "exfil-ask":
			exfilAsk++
		case "vuln-ask":
			vulnAsk++
		case "coder-inject":
			coderInjects++
		}
		if r.Surface == "PreToolUse/Agent" && (r.Action == "advise" || r.Action == "enforce") {
			routingCount++
			if r.Model != "" {
				if family, ok := cat.FamilyFor(r.Model); ok {
					familyCounts[family]++
				}
			}
		}
	}

	d.Kpis = []Kpi{
		{Label: "Decisions logged", Value: fmtInt(len(logs))},
		{Label: "Bytes filtered (measured)", Value: fmtBytes(measuredBytes), Sub: fmt.Sprintf("%d rewrites also estimated", rewrites)},
		{Label: "Routing decisions", Value: fmtInt(routingCount)},
	}
	if pr := buildPRStats(logs); pr != nil {
		d.PR = pr
		d.Kpis = append(d.Kpis, Kpi{Label: "PRs reviewed", Value: fmtInt(pr.Reviewed)})
	} else {
		d.Kpis = append(d.Kpis, Kpi{Label: "PRs reviewed", Value: "0", Sub: "run /deadeye-pr to start tracking"})
	}

	d.RoutingBars, d.RoutingTotal = buildFamilyBars(familyCounts)
	d.HygieneBars = buildRuleBars(perRuleMeasured)

	if exfilAsk > 0 {
		d.SecurityRows = append(d.SecurityRows, Kpi{Label: "Exfil guard -- asked", Value: fmtInt(exfilAsk)})
	}
	if vulnAsk > 0 {
		d.SecurityRows = append(d.SecurityRows, Kpi{Label: "Vulnerable dependency flagged", Value: fmtInt(vulnAsk)})
	}
	if coderInjects > 0 {
		d.SecurityRows = append(d.SecurityRows, Kpi{Label: "Coder mode session injections", Value: fmtInt(coderInjects)})
	}

	d.Surfaces = buildSurfaceCards(outcomes, repo, now)
	d.TrendChart = buildTrendChart(outcomes, repo, now)

	return d
}

// buildFamilyBars turns a family->count map into fixed-order categorical
// bars (sonnet, haiku, opus, fable -- never reordered by value, per the
// dataviz rule "color follows the entity"), each scaled against the
// group's own max.
func buildFamilyBars(counts map[string]int) ([]Bar, int) {
	order := []string{"sonnet", "haiku", "opus", "fable"}
	max := 0
	total := 0
	for _, n := range counts {
		if n > max {
			max = n
		}
		total += n
	}
	if total == 0 {
		// No routing row has resolved to a known family yet (empty log, or
		// every row predates the Model field) -- nil, not four zero bars,
		// so the template's empty state shows instead of a fake-looking
		// all-zero chart. Matches buildRuleBars/buildPRStats' same posture.
		return nil, 0
	}
	bars := make([]Bar, 0, len(order))
	for i, family := range order {
		n := counts[family]
		bars = append(bars, Bar{
			Label:   strings.ToUpper(family[:1]) + family[1:],
			Value:   fmtInt(n),
			Percent: percentOf(n, max),
			Color:   categoricalColors[i],
		})
	}
	return bars, total
}

// buildRuleBars renders internal/preprocess's real per-rule measured-bytes
// table (the same map gain.go's perRuleMeasured already computes) as
// sequential single-hue bars -- magnitude, not identity, so one hue per
// the dataviz form rules.
func buildRuleBars(perRule map[string][2]int) []Bar {
	if len(perRule) == 0 {
		return nil
	}
	max := 0
	for _, e := range perRule {
		if e[1] > max {
			max = e[1]
		}
	}
	rules := slices.Sorted(maps.Keys(perRule))
	bars := make([]Bar, 0, len(rules))
	for _, rule := range rules {
		e := perRule[rule]
		bars = append(bars, Bar{
			Label:   rule,
			Value:   fmtBytes(e[1]),
			Percent: percentOf(e[1], max),
		})
	}
	return bars
}

// buildSurfaceCards mirrors `deadeye lessons priority` and the
// SessionStart misses reminder exactly -- same RecentShapes calls, same
// 30-day window, same cap -- so the report never claims a signal those
// live surfaces wouldn't also be showing right now.
func buildSurfaceCards(outcomes []lessons.Outcome, repo string, now time.Time) []SurfaceCard {
	var cards []SurfaceCard

	var escalations, escalationWeight float64
	var escalationShape string
	for _, o := range outcomes {
		if o.EffectiveSurface() != lessons.SurfaceRouting || o.Kind != "escalation" {
			continue
		}
		escalations++
		escalationWeight += o.Weight
		escalationShape = o.TaskShape
	}
	routingNote := "No escalations recorded -- routing thresholds are unbiased."
	if escalations > 0 {
		routingNote = fmt.Sprintf("Downshift threshold raised for %s -- only makes the bar harder to clear, never easier.", escalationShape)
	}
	cards = append(cards, SurfaceCard{
		Name:  "Routing",
		Badge: fmt.Sprintf("%d escalation%s", int(escalations), plural(int(escalations))),
		Note:  routingNote,
	})

	coderShapes := lessons.RecentShapes(outcomes, lessons.SurfaceCoder, repo, now, featuredShapeCount)
	coderCard := SurfaceCard{Name: "Coder", Badge: fmt.Sprintf("%d miss%s", sumCount(coderShapes), pluralES(sumCount(coderShapes)))}
	for _, s := range coderShapes {
		coderCard.Shapes = append(coderCard.Shapes, ShapeCount{Shape: s.Shape, Count: s.Count})
	}
	if len(coderShapes) == 0 {
		coderCard.Note = "No coder-mode misses recorded yet in this repo."
	} else {
		coderCard.Note = "deadeye: recent misses in this repo: " + renderShapeList(coderShapes) + " -- recheck these before calling a change done."
	}
	cards = append(cards, coderCard)

	prShapes := lessons.RecentShapes(outcomes, lessons.SurfacePRReview, repo, now, featuredShapeCount)
	prCard := SurfaceCard{Name: "PR Review", Badge: fmt.Sprintf("%d disputed", sumCount(prShapes))}
	for _, s := range prShapes {
		prCard.Shapes = append(prCard.Shapes, ShapeCount{Shape: s.Shape, Count: s.Count})
	}
	if len(prShapes) == 0 {
		prCard.Note = "No disputed findings recorded yet in this repo."
	} else {
		prCard.Note = "Needs stronger proof: before these lenses report again -- never silenced outright."
	}
	cards = append(cards, prCard)

	return cards
}

// buildTrendChart plots the single highest-ranked coder-surface shape
// (coder-miss and external-miss combined, same as the SessionStart
// reminder) over trendWeeks, bucketed by calendar week-distance from now.
// Returns nil when there's under two distinct non-empty weeks of history --
// a one-point line isn't a trend, it's noise.
//
// viewBox is fixed at 640x220 (matching the approved mock): plot area
// x in [68,620], y in [30,180] with y=180 the zero baseline. valueToY(v) =
// 180 - (v/maxVal)*150, the same linear map the mock's hand-verified
// coordinates use.
func buildTrendChart(outcomes []lessons.Outcome, repo string, now time.Time) *TrendChart {
	ranked := lessons.RecentShapes(outcomes, lessons.SurfaceCoder, repo, now, 1)
	if len(ranked) == 0 {
		return nil
	}
	shape := ranked[0].Shape

	buckets := make([]int, trendWeeks) // index 0 = this week ... trendWeeks-1 = oldest
	var earliest time.Time
	nonEmptyWeeks := 0
	for _, o := range outcomes {
		if o.EffectiveSurface() != lessons.SurfaceCoder || o.Repo != repo || o.TaskShape != shape {
			continue
		}
		ts, err := time.Parse(time.RFC3339, o.TS)
		if err != nil {
			continue // unparseable timestamps can't be plotted on a time axis; still counted by RecentShapes above
		}
		if earliest.IsZero() || ts.Before(earliest) {
			earliest = ts
		}
		weeksAgo := int(now.Sub(ts).Hours() / 24 / 7)
		if weeksAgo < 0 || weeksAgo >= trendWeeks {
			continue
		}
		buckets[weeksAgo]++
	}
	for _, n := range buckets {
		if n > 0 {
			nonEmptyWeeks++
		}
	}
	if nonEmptyWeeks < 2 {
		return nil
	}

	maxVal := 0
	for _, n := range buckets {
		if n > maxVal {
			maxVal = n
		}
	}
	if maxVal < 2 {
		maxVal = 2 // keeps the axis (0/half/max) meaningful for a 0-or-1-per-week shape
	}

	const xLeft, xRight, yTop, yBottom = 68.0, 620.0, 30.0, 180.0
	valueToY := func(v int) float64 { return yBottom - (float64(v)/float64(maxVal))*(yBottom-yTop) }
	step := (xRight - xLeft) / float64(trendWeeks-1)

	chart := &TrendChart{
		Shape:    shape,
		MaxLabel: fmtInt(maxVal),
		MidLabel: fmtInt(maxVal / 2),
		MidY:     valueToY(maxVal / 2),
	}
	var line strings.Builder
	var area strings.Builder
	for i := trendWeeks - 1; i >= 0; i-- { // oldest to newest, left to right
		x := xLeft + step*float64(trendWeeks-1-i)
		y := valueToY(buckets[i])
		label := fmt.Sprintf("%d wks ago", i)
		if i == 0 {
			label = "now"
		}
		p := TrendPoint{Label: label, Value: buckets[i], X: x, Y: y, IsEndpoint: i == 0}
		chart.Points = append(chart.Points, p)
		if line.Len() > 0 {
			line.WriteByte(' ')
			area.WriteByte(' ')
		} else {
			area.WriteString(fmt.Sprintf("M %.1f,%.1f L ", x, y))
		}
		line.WriteString(fmt.Sprintf("%.1f,%.1f", x, y))
		area.WriteString(fmt.Sprintf("%.1f,%.1f", x, y))
	}
	area.WriteString(fmt.Sprintf(" L %.1f,%.1f L %.1f,%.1f Z", xRight, yBottom, xLeft, yBottom))
	chart.LinePoints = line.String()
	chart.AreaPath = area.String()

	if !earliest.IsZero() {
		weeksAgo := int(now.Sub(earliest).Hours() / 24 / 7)
		if weeksAgo >= trendWeeks {
			weeksAgo = trendWeeks - 1 // reminder predates the visible window -- pin to the oldest visible point rather than compute an off-chart position
		}
		if weeksAgo < 0 {
			weeksAgo = 0
		}
		chart.ReminderX = xLeft + step*float64(trendWeeks-1-weeksAgo)
		chart.HasReminder = true
	}

	return chart
}

// prSeverityOrder and prLensOrder fix the display order of PR-review
// breakdown bars -- severity worst-first, lens alphabetical -- so the
// report doesn't reorder them by count (INV-1's "color/order follows the
// entity" spirit applied to bars, not just chart hues).
var prSeverityOrder = []string{"critical", "high", "medium", "nit"}
var prLensOrder = []string{"security", "correctness", "over-engineering", "performance"}

// buildPRStats aggregates Surface=="PRReview" rows written by
// `deadeye report record ...` (cmd/deadeye/report.go). Returns nil when
// nothing has ever been recorded -- a repo that has never run /deadeye-pr
// gets no section, not a row of zeros implying it was tried and found empty.
func buildPRStats(logs []logstore.Record) *PRStats {
	var stats PRStats
	bySeverity := map[string]int{}
	byLens := map[string]int{}
	seenAny := false
	for _, r := range logs {
		if r.Surface != "PRReview" {
			continue
		}
		seenAny = true
		switch r.Action {
		case "reviewed":
			stats.Reviewed++
		case "posted":
			stats.Posted++
		case "skipped":
			stats.Skipped++
		case "finding":
			stats.Findings++
			lens, severity, ok := strings.Cut(r.Reason, ":")
			if !ok {
				continue
			}
			byLens[lens]++
			bySeverity[severity]++
		}
	}
	if !seenAny {
		return nil
	}

	lensMax := 0
	for _, n := range byLens {
		if n > lensMax {
			lensMax = n
		}
	}
	for _, lens := range prLensOrder {
		n := byLens[lens]
		if n == 0 {
			continue
		}
		stats.ByLens = append(stats.ByLens, Bar{Label: lensTitle(lens), Value: fmtInt(n), Percent: percentOf(n, lensMax)})
	}

	sevMax := 0
	for _, n := range bySeverity {
		if n > sevMax {
			sevMax = n
		}
	}
	for _, sev := range prSeverityOrder {
		n := bySeverity[sev]
		if n == 0 {
			continue
		}
		stats.BySeverity = append(stats.BySeverity, Bar{Label: strings.ToUpper(sev[:1]) + sev[1:], Value: fmtInt(n), Percent: percentOf(n, sevMax), Color: sev})
	}

	return &stats
}

func lensTitle(lens string) string {
	switch lens {
	case "over-engineering":
		return "Over-engineering"
	default:
		return strings.ToUpper(lens[:1]) + lens[1:]
	}
}

func sumCount(shapes []lessons.RankedShape) int {
	n := 0
	for _, s := range shapes {
		n += s.Count
	}
	return n
}

func renderShapeList(shapes []lessons.RankedShape) string {
	items := make([]string, len(shapes))
	for i, s := range shapes {
		items[i] = fmt.Sprintf("%s (%d×)", s.Shape, s.Count)
	}
	return strings.Join(items, ", ")
}

func percentOf(n, max int) int {
	if max <= 0 {
		return 0
	}
	p := n * 100 / max
	if p > 100 {
		p = 100
	}
	return p
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

func fmtInt(n int) string { return fmtBytes(n) }

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

// Render executes report.html.tmpl against d. html/template auto-escapes
// every field -- Reason strings ultimately trace back to tool-input-derived
// text (internal/coder/coder.go's own trust-boundary comment names this
// exact class of value), so this is the documented safe form, not
// text/template.
func Render(d Data) (string, error) {
	t, err := template.New("report").Parse(tmplSrc)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}
