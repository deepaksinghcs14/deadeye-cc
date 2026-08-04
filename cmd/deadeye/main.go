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
		fmt.Fprintln(os.Stderr, "usage: deadeye <hook|daemon|status|route|audit|capture|uninstall|version> [args]")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "hook":
		runHook(argOr(2, ""))
	case "daemon":
		runDaemon()
	case "status":
		runStatus()
	case "route":
		runRoute(strings.Join(os.Args[2:], " "))
	case "audit":
		runAudit()
	case "gain":
		runGain()
	case "lessons":
		runLessons(os.Args[2:])
	case "capture":
		runCapture(argOr(2, ""))
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
