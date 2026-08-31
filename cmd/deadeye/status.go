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

	if pv := pluginVersion(os.Getenv("CLAUDE_PLUGIN_ROOT")); pv != "" && semverNewer(pv, meta.Version) {
		fmt.Println(cWarn("⚠ binary " + meta.Version + " is BEHIND the installed plugin " + pv + " -- a `deadeye`"))
		fmt.Println(cWarn("  on your PATH is likely shadowing the managed one. Run `which deadeye`, then update"))
		fmt.Println(cWarn("  or remove it so ~/.deadeye/bin/deadeye takes over."))
		fmt.Println()
	}

	fmt.Println(cHead("Modes") + cDim("   change → deadeye config set <key> <value>"))
	srow("routing", cfg.Mode.Routing, "mode.routing", "off · advise · enforce")
	srow("effort", cfg.Mode.Effort, "mode.effort", "off · advise")
	srow("preprocess", cfg.Mode.Preprocess, "mode.preprocess", "off · on")
	srow("plan_gate", cfg.Mode.PlanGate, "mode.plan_gate", "off · soft · hard")
	srow("workflow_hint", cfg.Mode.WorkflowHint, "mode.workflow_hint", "off · on")
	srow("codemap", cfg.Mode.Codemap, "mode.codemap", "off · on")
	srow("update check", cfg.Mode.UpdateCheck, "mode.update_check", "off · on")
	fmt.Println()

	fmt.Println(cHead("Coder mode") + cDim("   change → /deadeye-coder <level>  or  deadeye config"))
	srow("persona", cfg.Coder.DefaultLevel, "coder.default_level", "off · spotter · marksman · sniper")
	srow("security check", cfg.Coder.Security, "coder.security", "off · advise · ask")
	srow("exfil guard", cfg.Security.Exfil, "security.exfil", "off · advise · ask")
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

	fmt.Println()
	fmt.Println(cDim("Change a setting:  deadeye config set <key> <value>   (interactive: deadeye config)"))
	fmt.Println(cDim("All commands:      /deadeye-help"))

	if level := os.Getenv("CLAUDE_EFFORT"); level != "" {
		fmt.Println()
		fmt.Println(cWarn(fmt.Sprintf("CLAUDE_EFFORT=%s is set: the effort axis is inert for this session -- deadeye's", level)))
		fmt.Println(cWarn("effort guidance is advisory only and cannot override a pinned level."))
	}
}

// srow prints one settings row: label, colored value, its config key, and the
// allowed values -- so /deadeye-status doubles as a "here's what to change and
// how" hub. Padding is computed on the RAW value length (color escapes don't
// count toward width), so the key column stays aligned.
func srow(label, value, key, allowed string) {
	pad := 12 - len(value)
	if pad < 1 {
		pad = 1
	}
	fmt.Printf("  %-15s %s%s%s %s\n",
		label, cMode(value), strings.Repeat(" ", pad),
		cDim(fmt.Sprintf("%-26s", key)), cDim(allowed))
}

func daemonStatus() string {
	conn, err := net.DialTimeout("unix", meta.SocketPath(), 100*time.Millisecond)
	if err != nil {
		return "down"
	}
	conn.Close()
	return "up (" + meta.SocketPath() + ")"
}
