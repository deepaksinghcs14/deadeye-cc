// Package secscan is coder mode's "check your backstop" lens made
// deterministic: a small, high-precision table of code and dependency
// patterns checked against an Edit/Write's ADDED text only, at PreToolUse.
// It never reads the target file and never scans the whole repo -- that's
// /deadeye-guard's job, which can read around a hunk and shell out to real
// auditors. This package exists to catch the handful of shapes a regex can
// see with near-zero false positives; anything that needs call-graph
// context (missing authz, path traversal, absence-of-a-guard) is
// deliberately left out, not attempted and hoped-for.
package secscan

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Finding is one advisory produced by Scan.
type Finding struct {
	Rule   string // stable name: log reason, config disable key, /deadeye-audit grouping
	Advice string // one line, deadeye voice, names the fix
}

// Rule is one code-pattern check. Match receives only the added text of an
// edit -- never the whole file -- so it can't reason about surrounding
// code. Exts scopes a rule to file extensions (lowercase, with the dot);
// nil means "any file". Multiple rows may share a Name to cover the same
// vulnerability class across languages with different idioms -- Scan
// reports each Name at most once per call.
type Rule struct {
	Name   string
	Exts   []string
	Advice string
	Match  func(added string) bool
}

func hasExt(exts []string, ext string) bool {
	for _, e := range exts {
		if e == ext {
			return true
		}
	}
	return false
}

// linesNear reports whether any line matching call also has a line within
// radius lines (inclusive, both directions) matching marker. Shared by
// rules where the dangerous call and the "this smells like a secret"
// context can straddle a line break -- weak-crypto is the one rule here
// that needs it; same-line rules just regexp.MatchString directly.
func linesNear(added string, call, marker *regexp.Regexp, radius int) bool {
	lines := strings.Split(added, "\n")
	for i, l := range lines {
		if !call.MatchString(l) {
			continue
		}
		lo, hi := i-radius, i+radius
		if lo < 0 {
			lo = 0
		}
		if hi >= len(lines) {
			hi = len(lines) - 1
		}
		for j := lo; j <= hi; j++ {
			if marker.MatchString(lines[j]) {
				return true
			}
		}
	}
	return false
}

// sqlKeyword anchors on the read/write verbs that make a query
// injectable -- DDL (CREATE/DROP) is rarely built from request-scoped
// input, so including it would only add false-positive surface.
const sqlKeyword = `\b(?:SELECT|INSERT|UPDATE|DELETE)\b`

// -- universal rules: Exts nil, fire in any file --------------------------

// secretPatternRe finds `key = "<literal>"` shapes for the usual credential
// field names. A regex alone over-fires on placeholders ("changeme",
// "your_api_key") -- matchSecretLiteral filters those in Go code instead of
// fighting the regex language for a lookaround RE2 doesn't have.
var (
	secretPatternRe = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|api[_-]?key|apikey|access[_-]?key|auth[_-]?token|token)\s*[:=]\s*["']([^"'\s]{8,})["']`)
	awsAccessKeyRe  = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	pemPrivateKeyRe = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
)

// placeholderSecrets are values that read as credentials but aren't --
// fixture data and doc examples use exactly these. Kept short and obvious;
// a placeholder deadeye doesn't recognize just gets flagged, which is the
// safe direction to be wrong in.
var placeholderSecrets = map[string]bool{
	"changeme": true, "change_me": true, "placeholder": true, "example": true,
	"dummy": true, "redacted": true, "your_api_key": true, "xxxxxxxx": true,
	"test1234": true, "password": true, "secret": true,
}

func matchSecretLiteral(added string) bool {
	if awsAccessKeyRe.MatchString(added) || pemPrivateKeyRe.MatchString(added) {
		return true
	}
	for _, m := range secretPatternRe.FindAllStringSubmatch(added, -1) {
		val := strings.ToLower(m[2])
		if placeholderSecrets[val] || strings.HasPrefix(val, "$") || strings.Contains(val, "${") {
			continue // env-var reference or known fixture value, not a real literal
		}
		return true
	}
	return false
}

// hashCallRe covers md5/sha1 constructors across all five languages in one
// alternation -- the call syntax is distinct enough per language that a
// false cross-language match isn't a realistic risk, so this stays
// Exts-unscoped rather than five near-duplicate rows.
var hashCallRe = regexp.MustCompile(`(?i)(md5\.(?:new|sum)|hashlib\.md5|MessageDigest\.getInstance\(\s*"MD5"|createHash\(\s*['"]md5['"]|md5::compute|Md5::new|sha1\.(?:new|sum)|hashlib\.sha1|MessageDigest\.getInstance\(\s*"SHA-?1"|createHash\(\s*['"]sha1['"]|Sha1::new)`)
var secretContextRe = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|credential|api[_-]?key)\b`)

// \b before "verify" matters: without it this matched ANY name merely
// ending in "verify" (e.g. a Python "email_verify = False" flag that has
// nothing to do with TLS) -- \b still matches the real pattern this is
// after (requests' "session.verify = False"), since "." is a non-word
// character and creates a boundary there same as whitespace or line start.
var tlsOffRe = regexp.MustCompile(`(?i)(InsecureSkipVerify\s*:\s*true|\bverify\s*=\s*False|rejectUnauthorized\s*:\s*false|danger_accept_invalid_certs\(\s*true\s*\)|ALLOW_ALL_HOSTNAME_VERIFIER|setHostnameVerifier\(\s*NoopHostnameVerifier)`)

// -- per-language idiom rules ---------------------------------------------

var (
	goSQLConcatRe  = regexp.MustCompile(`(?i)"[^"]*` + sqlKeyword + `[^"]*"\s*\+`)
	goSQLSprintfRe = regexp.MustCompile(`(?i)fmt\.Sprintf\(\s*"[^"]*` + sqlKeyword + `[^"]*%[a-zA-Z]`)
	goShellRe      = regexp.MustCompile(`exec\.Command\(\s*"(?:sh|bash|zsh|cmd|cmd\.exe)"\s*,\s*"-c"[^)]*\+`)

	jsSQLConcatRe   = regexp.MustCompile(`(?i)["'][^"']*` + sqlKeyword + `[^"']*["']\s*\+`)
	jsSQLTemplateRe = regexp.MustCompile("(?i)`[^`]*(?:" + sqlKeyword + "[^`]*\\$\\{|\\$\\{[^`]*" + sqlKeyword + ")")
	// The bare-call branch requires a non-word, non-dot character (or
	// string start) right before "exec": without it, this matched
	// RegExp.prototype's OWN .exec() -- e.g. `someRegex.exec(line + "\n")`,
	// completely unrelated to shelling out -- since ".exec(" is exactly
	// what a dotted method call looks like too. A first fix narrowed the
	// dotted-receiver branch to the literal "child_process." prefix only,
	// which closed that false positive but opened a false NEGATIVE: real,
	// idiomatic code just as often aliases the import (`const cp =
	// require('child_process')`, `import * as childProcess from
	// 'child_process'`) or destructures the module itself before calling
	// (`require('child_process').exec(...)`), none of which start with the
	// literal string "child_process." -- all silently stopped matching.
	// The receiver-name list below covers the common aliases explicitly
	// (a receiver this rule can't see the import for, like a custom name,
	// is the same no-call-graph-context limit every rule in this package
	// accepts); the require(...) alternative covers the no-variable form.
	// child_process's exec is otherwise called bare (destructured with no
	// receiver at all), covered by the last alternative. The receiver-name
	// group needs the SAME (?:^|[^.\w]) left-boundary guard as the bare-exec
	// alternative -- without it, "cp" (the shortest, riskiest alias) matched
	// as a bare substring anywhere a receiver name merely ENDED in "cp":
	// `scp.exec(cmd + arg)` (an scp helper) or `gcp.exec(cmd + arg)` (a
	// Google-Cloud-Platform client) both false-fired, neither referencing
	// child_process at all.
	jsShellRe      = regexp.MustCompile(`(?:(?:^|[^.\w])(?:child_process|childProcess|cp)\.exec(?:Sync)?|require\(\s*['"]child_process['"]\s*\)\s*\.exec(?:Sync)?|(?:^|[^.\w])exec(?:Sync)?)\(\s*[^,)]*(?:\+|\$\{)`)
	jsEvalRe       = regexp.MustCompile(`(?:\beval\(|new Function\()`)
	jsHTMLInjectRe = regexp.MustCompile(`(?:\.innerHTML\s*=[^=]|dangerouslySetInnerHTML|\bv-html\s*=)`)

	pySQLFStringRe  = regexp.MustCompile(`(?i)f["'][^"']*` + sqlKeyword + `[^"']*\{`)
	pySQLFormatRe   = regexp.MustCompile(`(?i)["'][^"']*` + sqlKeyword + `[^"']*\{\}[^"']*["']\s*\.format\(`)
	pySQLPercentRe  = regexp.MustCompile(`(?i)["'][^"']*` + sqlKeyword + `[^"']*["']\s*%\s*`)
	pySQLConcatRe   = regexp.MustCompile(`(?i)["'][^"']*` + sqlKeyword + `[^"']*["']\s*\+`)
	pyShellShellRe  = regexp.MustCompile(`subprocess\.[A-Za-z_]+\([^)]*shell\s*=\s*True`)
	pyShellSystemRe = regexp.MustCompile(`os\.system\(\s*[^)]*(?:\+|f["']|%\s*)`)
	pyEvalRe        = regexp.MustCompile(`\b(?:eval|exec)\(`)

	javaSQLConcatRe = regexp.MustCompile(`(?i)"[^"]*` + sqlKeyword + `[^"]*"\s*\+`)
	javaShellRe     = regexp.MustCompile(`Runtime\.getRuntime\(\)\.exec\(\s*[^,)]*\+`)
	javaEvalRe      = regexp.MustCompile(`\.eval\(`)
	javaDeserRe     = regexp.MustCompile(`(?:ObjectInputStream\b[\s\S]{0,200}\.readObject\(\)|enableDefaultTyping\()`)

	rustSQLFormatRe = regexp.MustCompile(`(?i)format!\(\s*"[^"]*` + sqlKeyword + `[^"]*\{\}`)
	rustShellRe     = regexp.MustCompile(`Command::new\(\s*"(?:sh|bash)"\s*\)\s*\.arg\(\s*"-c"\s*\)`)
)

var goExts = []string{".go"}
var jsExts = []string{".js", ".jsx", ".ts", ".tsx"}
var pyExts = []string{".py"}
var javaExts = []string{".java"}
var rustExts = []string{".rs"}

// Rules is the checked-in table. Order doesn't matter -- Scan runs every
// row and dedupes by Name.
var Rules = []Rule{
	{
		Name:   "secret-literal",
		Advice: "looks like a credential literal -- pull it from env/secret-store instead of committing it",
		Match:  matchSecretLiteral,
	},
	{
		Name:   "weak-crypto",
		Advice: "md5/sha1 near a password/token/secret -- neither is safe for that; use bcrypt/scrypt/argon2 for passwords, sha256+ for anything else",
		Match:  func(added string) bool { return linesNear(added, hashCallRe, secretContextRe, 3) },
	},
	{
		Name:   "tls-off",
		Advice: "TLS/cert verification disabled -- fine for a throwaway test, never for anything that leaves localhost",
		Match:  tlsOffRe.MatchString,
	},

	{Name: "sql-concat", Exts: goExts, Advice: "SQL built by concatenation -- bind the parameter instead; it's the shorter form", Match: func(a string) bool { return goSQLConcatRe.MatchString(a) || goSQLSprintfRe.MatchString(a) }},
	{Name: "sql-concat", Exts: jsExts, Advice: "SQL built by concatenation/interpolation -- bind the parameter instead; it's the shorter form", Match: func(a string) bool { return jsSQLConcatRe.MatchString(a) || jsSQLTemplateRe.MatchString(a) }},
	{Name: "sql-concat", Exts: pyExts, Advice: "SQL built by f-string/%/.format/concatenation -- pass the value as a query parameter instead", Match: func(a string) bool {
		return pySQLFStringRe.MatchString(a) || pySQLFormatRe.MatchString(a) || pySQLPercentRe.MatchString(a) || pySQLConcatRe.MatchString(a)
	}},
	{Name: "sql-concat", Exts: javaExts, Advice: "SQL built by concatenation -- use a PreparedStatement with bound parameters", Match: javaSQLConcatRe.MatchString},
	{Name: "sql-concat", Exts: rustExts, Advice: "SQL built via format! -- bind the parameter through the driver's query API instead", Match: rustSQLFormatRe.MatchString},

	{Name: "shell-interp", Exts: goExts, Advice: "shell string built for `sh -c` -- pass args to exec.Command directly, skip the shell", Match: goShellRe.MatchString},
	{Name: "shell-interp", Exts: jsExts, Advice: "exec() with an interpolated/concatenated command -- use execFile with an args array instead", Match: jsShellRe.MatchString},
	{Name: "shell-interp", Exts: pyExts, Advice: "shell=True or os.system with a built string -- pass args as a list, skip the shell", Match: func(a string) bool { return pyShellShellRe.MatchString(a) || pyShellSystemRe.MatchString(a) }},
	{Name: "shell-interp", Exts: javaExts, Advice: "Runtime.exec with a concatenated command -- use the array-args overload instead", Match: javaShellRe.MatchString},
	{Name: "shell-interp", Exts: rustExts, Advice: "sh -c hands the shell a string to parse -- pass the program and args directly to Command instead", Match: rustShellRe.MatchString},

	{Name: "eval-dynamic", Exts: jsExts, Advice: "eval/new Function on non-literal input -- there's almost always a non-eval way to do this", Match: jsEvalRe.MatchString},
	{Name: "eval-dynamic", Exts: pyExts, Advice: "eval/exec on non-literal input -- there's almost always a non-eval way to do this", Match: pyEvalRe.MatchString},
	{Name: "eval-dynamic", Exts: javaExts, Advice: "ScriptEngine.eval on non-literal input -- there's almost always a non-eval way to do this", Match: javaEvalRe.MatchString},

	{Name: "html-inject", Exts: jsExts, Advice: "raw HTML sink with a variable -- use textContent, or sanitize before innerHTML", Match: jsHTMLInjectRe.MatchString},

	{Name: "java-deser", Exts: javaExts, Advice: "unsafe deserialization sink -- validate the class allowlist, or avoid native (de)serialization for untrusted input", Match: javaDeserRe.MatchString},
}

// Scan returns findings for path's ADDED text (never the whole file).
// disabled is a Name-keyed skip set, same shape as
// config.DisabledRuleSet -- Scan takes it as a plain map rather than
// depending on the config package, keeping this package import-free of
// anything daemon-shaped.
func Scan(path, added string, disabled map[string]bool) []Finding {
	ext := strings.ToLower(filepath.Ext(path))
	var out []Finding
	seen := map[string]bool{}
	for _, r := range Rules {
		if disabled[r.Name] || seen[r.Name] {
			continue
		}
		if r.Exts != nil && !hasExt(r.Exts, ext) {
			continue
		}
		if r.Match(added) {
			out = append(out, Finding{Rule: r.Name, Advice: r.Advice})
			seen[r.Name] = true
		}
	}
	return out
}
