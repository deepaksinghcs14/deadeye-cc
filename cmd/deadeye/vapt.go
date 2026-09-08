package main

import (
	"fmt"
	"os"

	"github.com/deepaksinghcs14/deadeye-cc/internal/vaptreport"
)

// runVapt backs `deadeye vapt --in=<file>|- [--out=<path>]` -- the
// rendering step /deadeye-vapt's rubric calls after compiling its findings
// (internal/vapt/ruleset.md's "Report generation" section). Not a hook: a
// direct CLI invocation, so errors are reported and exit non-zero rather
// than failing open -- the rubric itself treats a failed render as
// non-blocking for the chat findings, not this command.
func runVapt(args []string) {
	in, rest := extractFlag(args, "--in=")
	out, _ := extractFlag(rest, "--out=")
	if in == "" {
		fmt.Fprintln(os.Stderr, "usage: deadeye vapt --in=<file>|- [--out=<path>]")
		os.Exit(2)
	}

	src := os.Stdin
	if in != "-" {
		f, err := os.Open(in)
		if err != nil {
			fmt.Fprintln(os.Stderr, "deadeye vapt:", err)
			os.Exit(1)
		}
		defer f.Close()
		src = f
	}

	if out == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "deadeye vapt:", err)
			os.Exit(1)
		}
		out = vaptreport.DefaultOutPath(cwd)
	}

	path, err := vaptreport.Generate(src, out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadeye vapt:", err)
		os.Exit(1)
	}
	fmt.Println(cGood("Wrote") + " " + path + cDim("   -- open it and Print → Save as PDF for a shareable copy"))
}
