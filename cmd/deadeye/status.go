package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// cMode colors a mode value by what it means: enforce/on active-green,
// advise/soft amber-ish neutral, off dim.
func cMode(v string) string {
	switch v {
	case "on", "enforce":
		return cGood(v)
	case "off":
		return cDim(v)
	default:
		return v
	}
}

// runStatus backs the /deadeye-status slash command.
func runStatus() {
	cfg := config.Load()
	cat := catalog.Load()
	records, _ := logstore.Scan(meta.LogPath())

	fmt.Printf("%s %s\n\n", cHead(meta.Name), cValue(meta.Version))

	fmt.Println(cHead("Modes"))
	fmt.Printf("  routing:       %s\n", cMode(cfg.Mode.Routing))
	fmt.Printf("  effort:        %s\n", cMode(cfg.Mode.Effort))
	fmt.Printf("  preprocess:    %s\n", cMode(cfg.Mode.Preprocess))
	fmt.Printf("  plan_gate:     %s\n", cMode(cfg.Mode.PlanGate))
	fmt.Printf("  workflow_hint: %s\n\n", cMode(cfg.Mode.WorkflowHint))

	fmt.Println(cHead("Coder mode"))
	fmt.Printf("  default level: %s\n", cValue(cfg.Coder.DefaultLevel))
	liveLevel := "inactive"
	if b, err := os.ReadFile(meta.CoderModePath()); err == nil {
		liveLevel = strings.TrimSpace(string(b))
	}
	if liveLevel == "inactive" {
		fmt.Printf("  live level:    %s\n", cDim(liveLevel))
	} else {
		fmt.Printf("  live level:    %s\n", cGood(liveLevel))
	}
	matcher := cfg.Coder.SubagentMatcher
	if matcher == "" {
		matcher = "(all subagents)"
	}
	fmt.Printf("  subagents:     %s\n\n", cDim(matcher))

	fmt.Println(cHead("Kill switches"))
	off := config.OffSwitches()
	for _, ev := range []string{"DEADEYE", "DEADEYE_PREPROCESS", "DEADEYE_GATE", "DEADEYE_CODER"} {
		state := cGood("on")
		for _, o := range off {
			if o == ev {
				state = cBad("OFF")
			}
		}
		fmt.Printf("  %-20s %s\n", ev, state)
	}
	fmt.Println()

	fmt.Println(cHead("Catalog") + cDim(fmt.Sprintf(" (%s, built %s)", cat.Source, cat.BuiltAt)))
	for _, m := range cat.Models {
		fmt.Printf("  tier %d  %-28s %s\n", m.Tier, m.ID, cDim(fmt.Sprintf("in $%.2f/MTok  out $%.2f/MTok", m.InputPrice, m.OutputPrice)))
	}
	fmt.Println()

	if d := daemonStatus(); strings.HasPrefix(d, "up") {
		fmt.Printf("%s %s\n", cHead("Daemon:"), cGood(d))
	} else {
		fmt.Printf("%s %s\n", cHead("Daemon:"), cBad(d))
	}
	fmt.Printf("%s %s %s\n", cHead("Log:"), meta.LogPath(), cDim(fmt.Sprintf("(%d records)", len(records))))

	if level := os.Getenv("CLAUDE_EFFORT"); level != "" {
		fmt.Println()
		fmt.Println(cWarn(fmt.Sprintf("CLAUDE_EFFORT=%s is set: the effort axis is inert for this session -- deadeye's", level)))
		fmt.Println(cWarn("effort guidance is advisory only and cannot override a pinned level."))
	}
}

func daemonStatus() string {
	conn, err := net.DialTimeout("unix", meta.SocketPath(), 100*time.Millisecond)
	if err != nil {
		return "down"
	}
	conn.Close()
	return "up (" + meta.SocketPath() + ")"
}
