package secscan

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ExfilMatch names a single exfiltration-guard hit. Pattern is the stable
// rule id (log reason, ask text); Path is the concrete credential path or
// token that matched, for the human-facing message.
type ExfilMatch struct {
	Pattern string
	Path    string
}

// ssh private keys are everything under ~/.ssh EXCEPT these non-secret
// housekeeping files and public halves.
var sshNonSecret = map[string]bool{
	"config": true, "known_hosts": true, "known_hosts.old": true,
	"authorized_keys": true,
}

// dotenvNonSecret are the .env.* suffixes that are placeholders, not real
// secret files.
var dotenvNonSecret = map[string]bool{
	"example": true, "sample": true, "template": true, "dist": true,
}

// MatchSensitiveReadPath reports whether absPath (already cleaned and, if it
// was relative, resolved against cwd by the caller) is a sensitive
// credential path -- a Read of which is the classic first step of
// prompt-injection-driven secret exfiltration. Existence is NEVER checked:
// a Read of a not-yet-existent key still reveals intent, and skipping the
// stat keeps this pure. home is the user's home dir; "" disables the
// home-anchored rules (dotenv and extra globs still apply). extra are
// additive user globs, matched against the full path and the basename.
func MatchSensitiveReadPath(absPath, home string, extra []string) (ExfilMatch, bool) {
	base := filepath.Base(absPath)

	// dotenv: not home-anchored -- a .env anywhere holds secrets, except
	// the well-known placeholder suffixes.
	if base == ".env" {
		return ExfilMatch{"dotenv", absPath}, true
	}
	if suffix, ok := strings.CutPrefix(base, ".env."); ok && !dotenvNonSecret[suffix] {
		return ExfilMatch{"dotenv", absPath}, true
	}

	if home != "" {
		if m, ok := matchHomeAnchored(absPath, base, home); ok {
			return m, true
		}
	}

	for _, g := range extra {
		if matchGlob(g, absPath) || matchGlob(g, base) {
			return ExfilMatch{"custom-sensitive-path", absPath}, true
		}
	}
	return ExfilMatch{}, false
}

func matchHomeAnchored(absPath, base, home string) (ExfilMatch, bool) {
	under := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	isUnder := func(dir string) bool {
		return strings.HasPrefix(absPath, dir+string(filepath.Separator))
	}

	switch {
	case isUnder(under(".ssh")) && !sshNonSecret[base] && !strings.HasSuffix(base, ".pub"):
		return ExfilMatch{"ssh-private-key", absPath}, true
	case absPath == under(".aws", "credentials"): // NOT .aws/config -- routine region settings
		return ExfilMatch{"aws-credentials", absPath}, true
	case absPath == under(".claude", ".credentials.json"):
		return ExfilMatch{"claude-credentials", absPath}, true
	case absPath == under(".netrc") || absPath == under("_netrc"):
		return ExfilMatch{"netrc", absPath}, true
	case absPath == under(".kube", "config"):
		return ExfilMatch{"kube-config", absPath}, true
	case absPath == under(".docker", "config.json"):
		return ExfilMatch{"docker-config", absPath}, true
	case absPath == under(".git-credentials"):
		return ExfilMatch{"git-credentials", absPath}, true
	case absPath == under(".npmrc"): // home-anchored only; a project .npmrc is routine registry config
		return ExfilMatch{"npmrc", absPath}, true
	case absPath == under(".pypirc"):
		return ExfilMatch{"pypirc", absPath}, true
	case absPath == under(".config", "gcloud", "application_default_credentials.json") ||
		absPath == under(".config", "gcloud", "credentials.db") ||
		isUnder(under(".config", "gcloud", "legacy_credentials")):
		return ExfilMatch{"gcloud-credentials", absPath}, true
	case absPath == under(".config", "gh", "hosts.yml"):
		return ExfilMatch{"gh-hosts", absPath}, true
	}
	return ExfilMatch{}, false
}

// matchGlob is filepath.Match with a compile failure treated as no-match
// (fail open -- a user's bad glob never crashes or over-fires).
func matchGlob(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}

// -- Bash egress shapes ---------------------------------------------------

var (
	readerBinRe = regexp.MustCompile(`\b(?:cat|head|tail|less|more|base64|xxd|od|strings)\b`)
	// ssh is deliberately NOT here: connecting with a key is not shipping
	// it. scp/rsync ARE, guarded by the -i/-e identity-flag exception below.
	netBinRe = regexp.MustCompile(`\b(?:curl|wget|nc|ncat|scp|rsync|ftp|sftp)\b`)
	// A pipeline segment that is JUST an env dump -- not `env FOO=bar cmd`,
	// which is a prefix-runner. Optional simple flags allowed.
	envDumpSegRe = regexp.MustCompile(`^\s*(?:env|printenv)(?:\s+-[A-Za-z]+)*\s*$`)
)

// sensitiveShellFrag are the credential subpaths recognized inside shell
// text, appearing after a home anchor (~, $HOME, ${HOME}, or the absolute
// home path). Kept to the high-signal ones -- the same table as the Read
// guard, expressed as shell fragments.
var sensitiveShellFrag = []string{
	".ssh/id_", ".ssh/identity", ".aws/credentials", ".claude/.credentials",
	".netrc", ".kube/config", ".docker/config.json", ".git-credentials",
	".npmrc", ".pypirc", ".config/gcloud", ".config/gh/hosts",
}

// sensitiveTokenRe builds the regex that finds a sensitive credential
// reference in shell text for the given home. Anchors: ~/ , $HOME/ ,
// ${HOME}/ , or the literal absolute home path; plus a bare .env word.
func sensitiveTokenRe(home string) *regexp.Regexp {
	anchors := []string{`~`, `\$HOME`, `\$\{HOME\}`}
	if home != "" {
		anchors = append(anchors, regexp.QuoteMeta(home))
	}
	frags := make([]string, len(sensitiveShellFrag))
	for i, f := range sensitiveShellFrag {
		frags[i] = regexp.QuoteMeta(f)
	}
	homeRef := `(?:` + strings.Join(anchors, "|") + `)/` + `(?:` + strings.Join(frags, "|") + `)`
	// bare .env as a standalone word, not .env.example etc.
	dotenv := `(?:^|[\s'"=@])\.env\b(?:\.(?:local|development|dev|production|prod|staging|test))?`
	return regexp.MustCompile(homeRef + `|` + dotenv)
}

// MatchExfilBash reports whether cmd is a credential-egress shape. Three
// conservative heuristics, checked most-severe first. Documented ceilings:
// grep-based reads and shell obfuscation (p=~/.ssh; cat $p/id_rsa) are not
// chased -- only regex-visible shapes, same contract as this package's doc.
func MatchExfilBash(cmd, home string, extra []string) (ExfilMatch, bool) {
	tokenRe := sensitiveTokenRe(home)
	hasNet := netBinRe.MatchString(cmd)

	// B2 sensitive-path-egress (most severe): a sensitive token paired with
	// a network binary -- UNLESS every sensitive token is an -i/-e identity
	// argument (using a key to connect isn't exfiltrating it).
	if hasNet {
		for _, loc := range tokenRe.FindAllStringIndex(cmd, -1) {
			if !precededByIdentityFlag(cmd, loc[0]) {
				return ExfilMatch{"sensitive-path-egress", strings.TrimSpace(cmd[loc[0]:loc[1]])}, true
			}
		}
	}

	// B3 env-dump-egress: `env`/`printenv` alone in a pipeline segment,
	// piped into a network binary in a LATER segment.
	if hasNet {
		segs := strings.Split(cmd, "|")
		for i, seg := range segs {
			if envDumpSegRe.MatchString(seg) {
				for _, later := range segs[i+1:] {
					if netBinRe.MatchString(later) {
						return ExfilMatch{"env-dump-egress", "env|" + strings.TrimSpace(later)}, true
					}
				}
			}
		}
	}

	// B1 sensitive-read-shell: a reader binary pulling a sensitive path into
	// context (the trivial Read-tool bypass, e.g. `cat ~/.ssh/id_rsa`).
	if readerBinRe.MatchString(cmd) {
		if loc := tokenRe.FindStringIndex(cmd); loc != nil {
			return ExfilMatch{"sensitive-read-shell", strings.TrimSpace(cmd[loc[0]:loc[1]])}, true
		}
	}
	return ExfilMatch{}, false
}

// precededByIdentityFlag reports whether the token starting at pos is the
// argument of an -i or -e flag (ssh/scp/rsync identity/rsh) -- a key USED
// to connect, not shipped out.
func precededByIdentityFlag(cmd string, pos int) bool {
	before := strings.TrimRight(cmd[:pos], " ")
	return strings.HasSuffix(before, "-i") || strings.HasSuffix(before, "-e")
}
