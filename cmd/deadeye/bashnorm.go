package main

import "strings"

// bashRetryStreak is the same-key run count that fires the bash-retry
// advisory: one flag-escalation re-run after a failure (-v, -x) is routine
// debugging; the third run of an unchanged target with no edit in between
// is the loop.
const bashRetryStreak = 3

// normalizeBashRetryKey reduces a simple Bash command to a retry key: the
// binary plus its non-option arguments, so runs of the SAME target that
// differ only in flags share a key (`go test ./pkg`, `go test -v ./pkg`,
// `go test -race -v ./pkg`). ok=false means the command is out of scope
// and retry detection stays inert -- the precision backstop, since a false
// "you're looping" is worse than a miss:
//
//   - compound/complex commands (pipes, chains, redirects, substitutions)
//     are refused outright rather than parsed;
//   - -opt=value keeps the VALUE in the key, so `-run=TestFoo` vs
//     `-run=TestBar` never collide;
//   - a bare value after a spaced option (`-n 5`) is kept as a target, so
//     `-n 5` vs `-n 10` differ -- miss-side bias, by design;
//   - single-token commands are refused: an exact repeat of those is
//     already the repeat-command rule's territory.
func normalizeBashRetryKey(cmd string) (key string, ok bool) {
	if strings.ContainsAny(cmd, "|&;<>`\n") || strings.Contains(cmd, "$(") {
		return "", false
	}
	fields := strings.Fields(cmd)
	if len(fields) < 2 {
		return "", false
	}
	parts := []string{fields[0]}
	for _, f := range fields[1:] {
		if !strings.HasPrefix(f, "-") {
			parts = append(parts, f)
			continue
		}
		if _, val, found := strings.Cut(f, "="); found && val != "" {
			parts = append(parts, val)
		}
	}
	return strings.Join(parts, " "), true
}
