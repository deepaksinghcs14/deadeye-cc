package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/coder"
	"github.com/deepaksinghcs14/deadeye-cc/internal/config"
)

const initUsage = "usage: deadeye init <codex|gemini|cursor|windsurf> [--yes]"

// rulesFileRelPath maps a rules-file host to where its always-on rules file
// lives, relative to the project root. These hosts have NO hook contract,
// so deadeye can only give them the coder persona as a static file.
var rulesFileRelPath = map[string]string{
	"cursor":   filepath.Join(".cursor", "rules", "deadeye.md"),
	"windsurf": filepath.Join(".windsurf", "rules", "deadeye.md"),
}

// runInitRules writes deadeye's coder ruleset as a static rules file for a
// hook-less host (cursor, windsurf). Project-local: it lands under the
// current directory, the project the user is setting up. Persona only --
// no routing, security, preprocessing, or codemap engine.
func runInitRules(host string, args []string) {
	rel, ok := rulesFileRelPath[host]
	if !ok {
		fmt.Fprintln(os.Stderr, initUsage)
		os.Exit(2)
	}
	assumeYes := false
	for _, a := range args {
		if a == "--yes" || a == "-y" {
			assumeYes = true
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init "+host+":", err)
		os.Exit(1)
	}
	path := filepath.Join(cwd, rel)

	// Never overwrite a rules file deadeye didn't write.
	if !rulesFileIsOursOrAbsent(path) {
		fmt.Fprintln(os.Stderr, "deadeye init "+host+": "+path+" exists and isn't deadeye's -- not overwriting.")
		os.Exit(1)
	}

	level := coder.LevelMarksman
	if l, ok := coder.NormalizePersisted(config.Load().Coder.DefaultLevel); ok {
		level = l
	}
	body := coder.RulesetMarkdown(level)

	fmt.Println(cHead("deadeye init "+host) + cDim("  (persona only -- "+host+" has no hook contract for the engine)"))
	fmt.Println()
	fmt.Println("Will write the coder ruleset (level " + cValue(level) + ") to " + cValue(path))
	fmt.Println(cDim("The lean-coding discipline alone. Routing, security, and preprocessing need Claude Code, Codex, or Gemini CLI."))
	fmt.Println()
	if !assumeYes {
		fmt.Print("Apply? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
			fmt.Println("Nothing written.")
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init "+host+":", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye init "+host+":", err)
		os.Exit(1)
	}
	fmt.Println(cGood("Wrote") + " " + path)
	installPRCommand(host)
	fmt.Println(cDim("Remove any time with: deadeye uninstall " + host))
}

// rulesFileIsOursOrAbsent reports whether path is safe for init to write:
// true if the file is absent or already carries deadeye's marker, false
// for a file the user authored. The whole "never clobber a foreign rules
// file" guard, made testable without the os.Exit call sites.
func rulesFileIsOursOrAbsent(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return true // absent
	}
	return strings.Contains(string(b), coder.RulesetMarkdownMarker)
}

// runUninstallRules removes the rules file init wrote, only if it still
// carries deadeye's marker (so a user's own rules file is never deleted).
func runUninstallRules(host string) {
	rel := rulesFileRelPath[host]
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall "+host+":", err)
		os.Exit(1)
	}
	path := filepath.Join(cwd, rel)
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Nothing to remove (" + path + " not present).")
		return
	}
	if !strings.Contains(string(b), coder.RulesetMarkdownMarker) {
		fmt.Fprintln(os.Stderr, "deadeye uninstall "+host+": "+path+" isn't deadeye's -- leaving it.")
		os.Exit(1)
	}
	if err := os.Remove(path); err != nil {
		fmt.Fprintln(os.Stderr, "deadeye uninstall "+host+":", err)
		os.Exit(1)
	}
	removePRCommand(host)
	fmt.Println(cGood("Removed") + " " + path)
}
