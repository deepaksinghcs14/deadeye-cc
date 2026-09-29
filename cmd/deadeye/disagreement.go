package main

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/lessons"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runDisagreement reports how often the judge would have routed a task
// cheaper than the kernel confidently did -- the over-route blind spot
// (see maybeSampleTier). Global across repos, like `deadeye gain`: routing
// heuristics are machine-wide, not per-project.
func runDisagreement() {
	renderDisagreement(meta.OutcomesPath(), config.Load(), time.Now())
}

func renderDisagreement(outcomesPath string, cfg config.Config, now time.Time) {
	outs, err := lessons.Scan(outcomesPath)
	if err != nil {
		fmt.Println("deadeye disagreement:", err)
		return
	}

	type shapeStat struct {
		n        int
		toTier   map[int]int
		exemplar string
	}
	perShape := map[string]*shapeStat{}
	total := 0
	escalations := 0
	for _, o := range outs {
		if o.TS != "" {
			if ts, perr := time.Parse(time.RFC3339, o.TS); perr == nil && now.Sub(ts) > judgedTierAge {
				continue
			}
		}
		switch o.Kind {
		case KindTierDisagreement:
			total++
			s := perShape[o.TaskShape]
			if s == nil {
				s = &shapeStat{toTier: map[int]int{}}
				perShape[o.TaskShape] = s
			}
			s.n++
			s.exemplar = o.Model
			if o.JudgedTier != nil {
				s.toTier[*o.JudgedTier]++
			}
		case "escalation":
			escalations++
		}
	}

	fmt.Println("  " + cHead("deadeye disagreement") + cDim("  where the judge would have routed cheaper"))
	fmt.Println()

	if cfg.Mode.TierSample != "on" {
		fmt.Println("  " + cWarn("Sampling is off.") + cDim(" Nothing is being measured right now."))
		fmt.Println("  " + cDim("Turn it on with") + " " + cValue("deadeye config set mode.tier_sample on"))
		fmt.Printf("  %s\n", cDim(fmt.Sprintf("Each sample spends one `claude -p` judge call; rate is 1-in-%d.", sampleRate(cfg))))
		fmt.Println()
	}

	if total == 0 {
		fmt.Println("  " + cDim("No sampled disagreements in the last 30 days."))
		if cfg.Mode.TierSample == "on" {
			fmt.Println("  " + cDim("Either the heuristics and the judge agree on confident high-tier"))
			fmt.Println("  " + cDim("routes, or not enough of them have been sampled yet. Both look the"))
			fmt.Println("  " + cDim("same here -- this is a count of disagreements, not a pass rate."))
		}
		return
	}

	fmt.Printf("  %s   %s in the last 30 days %s\n", cHead("Disagreements"), cWarn(fmt.Sprintf("%d", total)), cDim("(1-in-"+fmt.Sprintf("%d", sampleRate(cfg))+" sample of confident tier-2+ routes)"))
	if escalations > 0 {
		fmt.Printf("  %s     %d in the same window %s\n", cHead("Escalations"), escalations, cDim("(the opposite direction -- routed too cheap)"))
	}
	fmt.Println()
	fmt.Println("  " + cHead("By task shape"))
	for _, shape := range slices.Sorted(maps.Keys(perShape)) {
		s := perShape[shape]
		tiers := slices.Sorted(maps.Keys(s.toTier))
		detail := ""
		for _, t := range tiers {
			detail += fmt.Sprintf(" tier%d x%d", t, s.toTier[t])
		}
		fmt.Printf("    %-28s %s routed %s, judge said%s\n", shape, cValue(fmt.Sprintf("%dx", s.n)), s.exemplar, detail)
	}

	fmt.Println()
	fmt.Println(cDim("  This is the judge's OPINION, not a measured over-route rate. A cheaper"))
	fmt.Println(cDim("  tier might still have failed the task -- an arbitrary production subtask"))
	fmt.Println(cDim("  has no grader, which is why this samples a second opinion instead of"))
	fmt.Println(cDim("  replaying the work and scoring it. benchmarks/routing/ is where real"))
	fmt.Println(cDim("  graded comparison happens, on fixtures that ship hidden tests."))
	fmt.Println(cDim("  Nothing here changes routing: escalation bias stays one-directional"))
	fmt.Println(cDim("  until this number has been trusted for a while."))
}

func sampleRate(cfg config.Config) int {
	if cfg.TierSample.Rate < 1 {
		return 10
	}
	return cfg.TierSample.Rate
}
