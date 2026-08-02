package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/deepaksinghcs14/deadeye-cc/internal/catalog"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
	"github.com/deepaksinghcs14/deadeye-cc/internal/logstore"
	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

// runStatus backs the /deadeye-status slash command.
func runStatus() {
	cfg := config.Load()
	cat := catalog.Load()
	records, _ := logstore.Scan(meta.LogPath())

	fmt.Printf("%s %s\n\n", meta.Name, meta.Version)

	fmt.Println("Modes:")
	fmt.Printf("  routing:       %s\n", cfg.Mode.Routing)
	fmt.Printf("  effort:        %s\n", cfg.Mode.Effort)
	fmt.Printf("  preprocess:    %s\n", cfg.Mode.Preprocess)
	fmt.Printf("  plan_gate:     %s\n", cfg.Mode.PlanGate)
	fmt.Printf("  workflow_hint: %s\n", cfg.Mode.WorkflowHint)
	fmt.Printf("  posture:       %s\n\n", cfg.Posture)

	fmt.Println("Kill switches:")
	for _, ev := range []string{"DEADEYE", "DEADEYE_PREPROCESS", "DEADEYE_GATE"} {
		state := "on"
		if config.KillSwitchOff(ev) {
			state = "OFF"
		}
		fmt.Printf("  %-20s %s\n", ev, state)
	}
	fmt.Println()

	fmt.Printf("Catalog (%s, built %s):\n", cat.Source, cat.BuiltAt)
	for _, m := range cat.Models {
		fmt.Printf("  tier %d  %-28s in $%.2f/MTok  out $%.2f/MTok\n", m.Tier, m.ID, m.InputPrice, m.OutputPrice)
	}
	fmt.Println()

	fmt.Printf("Daemon: %s\n", daemonStatus())
	fmt.Printf("Log:    %s (%d records)\n", meta.LogPath(), len(records))

	if level := os.Getenv("CLAUDE_EFFORT"); level != "" {
		fmt.Printf("\nCLAUDE_EFFORT=%s is set: the effort axis is inert for this session -- deadeye's\n", level)
		fmt.Println("effort guidance is advisory only and cannot override a pinned level.")
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
