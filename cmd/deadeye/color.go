package main

import "os"

// Minimal ANSI styling for CLI output: on when stdout is a terminal OR
// the caller forces it via the standard FORCE_COLOR convention -- Claude
// Code sets FORCE_COLOR=3 in its Bash sessions and renders ANSI in the
// output pane, so /deadeye-* command output colors there too. NO_COLOR
// always wins; plain pipes (tests, scripts, the hook path) stay plain.
var colorOn = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if f := os.Getenv("FORCE_COLOR"); f != "" && f != "0" {
		return true
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}()

func paint(code, s string) string {
	if !colorOn {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func cHead(s string) string  { return paint("1;38;5;179", s) } // brass, bold -- section headers
func cGood(s string) string  { return paint("38;5;108", s) }   // green -- on/up/measured
func cWarn(s string) string  { return paint("38;5;173", s) }   // amber -- estimates, cautions
func cBad(s string) string   { return paint("38;5;167", s) }   // red -- OFF/down
func cDim(s string) string   { return paint("38;5;245", s) }   // grey -- annotations
func cValue(s string) string { return paint("1", s) }          // bold -- key values
