// Command deadeye is the deadeye plugin's single binary: hook client,
// daemon, and CLI, dispatched by subcommand.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/deepaksinghcs14/deadeye-cc/internal/meta"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: deadeye <hook|daemon|status|config|route|audit|gain|context|lessons|report|init|capture|uninstall|version> [args]")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hook":
		runHook(argOr(2, ""), argsFrom(3))
	case "daemon":
		runDaemon()
	case "status":
		runStatus()
	case "config":
		runConfig(os.Args[2:])
	case "route":
		subagentType, rest := extractFlag(os.Args[2:], "--subagent-type=")
		runRoute(strings.Join(rest, " "), subagentType)
	case "audit":
		runAudit()
	case "gain":
		runGain()
	case "lessons":
		runLessons(os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	case "capture":
		runCapture(argOr(2, ""))
	case "context":
		runContext(argOr(2, ""))
	case "init":
		runInit(os.Args[2:])
	case "update":
		runUpdate()
	case "notes-append":
		runNotesAppend(argOr(2, ""))
	case "uninstall":
		runUninstall(os.Args[2:])
	case "version":
		fmt.Println(meta.Name, meta.Version)
	default:
		fmt.Fprintf(os.Stderr, "deadeye: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func argOr(i int, def string) string {
	if i < len(os.Args) {
		return os.Args[i]
	}
	return def
}

// argsFrom returns os.Args[i:], or nil if the command line is shorter than
// i -- a bare "deadeye hook" (no event, no args) must fail open like every
// other malformed hook invocation, not panic with a slice-bounds crash.
func argsFrom(i int) []string {
	if i > len(os.Args) {
		return nil
	}
	return os.Args[i:]
}

// extractFlag pulls the first "prefix<value>" token out of args (e.g.
// "--subagent-type=Explore"), returning the value and the remaining args
// with that token removed. Absent -- the common case -- returns "" and args
// unchanged.
func extractFlag(args []string, prefix string) (string, []string) {
	for i, a := range args {
		if strings.HasPrefix(a, prefix) {
			rest := append(append([]string{}, args[:i]...), args[i+1:]...)
			return strings.TrimPrefix(a, prefix), rest
		}
	}
	return "", args
}
