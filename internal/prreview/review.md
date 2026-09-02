<!-- deadeye-review: canonical rubric; edit internal/prreview/review.md, the skill and every host rendering are generated from it -->
# Deadeye Review

Review code through four lenses — over-engineering, correctness,
performance, security — the same rubric `/deadeye-pr` runs on a whole pull
request, scoped instead to your working diff or the whole repo. This is the
local, pre-PR self-review: catch what would otherwise wait for a PR (or a
bot) to find. For a real GitHub PR, use `/deadeye-pr`. For a deeper,
dependency-audit-backed security-only pass, use `/deadeye-guard`.

Two scopes:

- **default** — the current working diff.
- **`--repo`** (or "audit the whole repo") — the entire repository, ranked
  worst-first across all four lenses. See "Whole-repo mode" below.

## Scope (default: the working diff)

Get the diff with `git diff` (or `git diff --staged` if the user says
staged, or `git diff <ref>` for a named base). Read the changed hunks
**plus enough surrounding context to judge a trust boundary or a caller
contract** — "is this input validated" and "does this break a caller" both
need the code around the hunk, not just the `+` lines.

- Empty diff (nothing changed or staged): say so plainly and stop — do
  not substitute a different scope.
- Not a git repo: ask the user which files to review.

Before tagging `yagni:`/`delete:`, or claiming an `authz`/nil/sanitizer
check is MISSING, grep for implementers/callers/guards OUTSIDE the diff —
an "interface with one impl" whose second impl lives in a test file, or a
guard that lives one call down, is a false positive. Report only what you
confirmed.

{{lenses}}

## Whole-repo mode (`--repo`)

Scan the whole repository through all four lenses and report a ranked
list — worst-first, most severe finding leads regardless of lens:

`<glyph> [path] — <tag>: <what actually happens, concretely>. Fix: <fix>.`

Same tags, same proof discipline as the diff mode above.

**Scope cheaply — token thrift is this plugin's whole point:**

1. Enumerate with `git ls-files` (or `find` with `-maxdepth` if not a git
   repo) — never by reading directories of files whole.
2. Grep-first for candidates before opening ANY file body:
   - over-engineering — duplicate deps (`go.mod`/`package.json` vs stdlib),
     `interface` declarations (then `grep -c` their implementers),
     one-export files, config keys (then grep for readers), wrapper-shaped
     names (`*Wrapper`, `*Manager`, `*Factory`, `*Helper`)
   - correctness/performance — unbounded loops, `O(n²)`-shaped nested
     iteration over slices/maps, resource-open calls (`Open`/`Dial`/`Begin`)
     without a nearby `Close`/`defer`, goroutines/threads touching shared
     state
   - security — raw SQL/shell/template/`eval` call sites, URL-fetch calls
     on user-controlled input, hand-rolled crypto (`md5`/`sha1` near
     "password"/"token"), a dependency manifest or lockfile that changed
     recently (`git log -1 --format=%ct go.sum package-lock.json`)
3. Read full file contents ONLY for the top candidates you intend to list —
   a sweep that reads the whole repo into context is the exact waste this
   plugin exists to prevent.

If a dependency manifest exists, run its native auditor when installed
(`govulncheck ./...`, `npm audit`, `pip-audit`, `cargo audit`, or
`osv-scanner -L <manifest>` if none is) rather than reading the dependency
tree into context — same discipline as `/deadeye-guard`'s dependency pass.

**Verify before reporting:** grep for ALL implementers/callers/guards across
the repo (including test files and other packages) — "interface with one
implementation" must mean one implementer exists, not one you happened to
find. Report only what you confirmed.

**Output discipline:** rank by severity first, then impact within a
severity. Cap at 20 findings — fewer exist → stop, never pad; more → keep
the 20 worst and say how many were omitted. This is a ranked sample of a
whole repo, not exhaustive coverage — never report partial coverage as
complete. Nothing found: exactly `Clean — nothing survived verification.`
If a replacement is itself a deliberate simplification with a known
ceiling, plant the marker line: `# deadeye: <shortcut>. ceiling: <limit>.
upgrade: <trigger>.` Skip vendored code, generated code, and lockfiles.

## Learning loop (repo-scoped priority)

Before finalizing, run `deadeye lessons priority` (best-effort — if
`deadeye` isn't on PATH, retry once with `~/.deadeye/bin/deadeye`; if that
also fails, review normally). It prints this repo's recent signal, if any:

- **Recent coder misses** — scrutinize those lens/tags harder; a shape that
  slipped through before is worth a second look.
- **Recently disputed findings** — need stronger `proof:` before reporting
  that lens/tag again. Never skip it outright: one dismissal doesn't retire
  a whole tag, it only raises the bar for the next one.

For each finding that survives verification and makes your final report
(never a candidate you dropped), record it so coder mode gets reminded next
session (best-effort, same retry-once contract as above):

```bash
deadeye lessons record coder-miss <lens>:<tag>
```

using the lens the finding came from (`over-engineering`, `correctness`,
`performance`, or `security`) and its tag without the trailing colon — e.g.
a `race:` finding → `deadeye lessons record coder-miss correctness:race`.
This is a no-op when coder mode wasn't active this session — nothing to
attribute, nothing gets written. Diff-scope only — `--repo` mode above scans
pre-existing code nothing here wrote this session, so it never attributes to
coder mode.

When the user disputes a finding you reported ("that's not a bug",
"already handled", "won't fix"), record it so the next review on this repo
weighs that lens/tag accordingly:

```bash
deadeye lessons record review-false-positive <lens>:<tag>
```

## Output

Lead with a one-line header, then the four lens sections, then a verdict:

```
<files> files, +<adds>/-<dels>
```

(`git diff --shortstat` gives you the numbers; omit the header entirely in
`--repo` mode, where the ranked list above is the output.)

End with the tally and the verdict — `<C> critical, <H> high, <M> medium,
<N> nits` and the one `critical` that must ship fixed — or, when nothing
survived verification, exactly: `Clean — nothing survived verification.
Ship it.`

Findings are a LIST. Do not apply or push any code change unless asked.

{{fixes}}

## Boundaries

- Findings are a LIST. Do not apply them unless asked.
- Never flag the one runnable check coder mode leaves behind for
  deletion — lean code without its check is unfinished.
- Log spam is over-instrumentation, cut it (a line per loop, a metric nobody
  reads) — but never flag the one breadcrumb at a real failure boundary as
  bloat; a wrapped error or the log where it fails is load-bearing, like the
  runnable check.
- All four lenses are in scope here. `/deadeye-guard` is the deeper,
  dedicated security pass (native dependency auditors, a wider weakest-path
  sweep) for when security alone is the ask; `/deadeye-pr` is this same
  rubric run against a real GitHub PR.
