<!-- deadeye-pr: canonical rubric; edit internal/prreview/ruleset.md, the skill and every host rendering are generated from it -->
# Deadeye PR Review

One shot over a whole pull request: four lenses, one pass, tagged findings.
`/deadeye-review` runs this exact four-lens rubric locally against your
working diff or the whole repo — this adds what a PR needs on top:
resolving a real PR via `gh`, checking what other reviewers already said,
huge-PR fan-out, and an opt-in post back to GitHub. `/deadeye-guard` stays
the dedicated deep-security pass this lens is drawn from.

## Scope

Resolve the target PR, then review only its diff:

- An argument (a PR number like `123` or a full PR URL, after stripping
  `--post`) → that PR.
- No argument → the PR for the current branch.
- Fetch the diff and metadata with the GitHub CLI:
  - `gh pr diff <N>` (or `gh pr diff` for the current branch) for the unified diff.
  - `gh pr view <N> --json title,body,additions,deletions,files,baseRefName,headRefName` for the header.
- Read the changed hunks **plus enough surrounding context to judge a trust
  boundary or a caller contract** — "is this input validated" and "does this
  break a caller" both need the code around the hunk, not just the `+` lines.

Preconditions and graceful degradation:

- `gh` not installed or not authenticated → say so plainly and stop, or, if
  the user has a local branch, offer to review `git diff <base>...HEAD`
  instead. Do not invent PR contents.
- Not a GitHub repo / no PR for the branch → say so; don't substitute a
  different scope.
- Huge PR (more than ~40 changed files or a few thousand lines) → review it ALL
  by fanning out one subagent per ~2,500-line package-grouped cluster, in
  parallel, each returning findings in the standard format. Spawn each cluster
  subagent at the cheapest tier that fits it, but the review floor is tier 1
  (sonnet) for any cluster with real logic — drop to tier 0 only for a purely
  mechanical cluster (generated code, lockfiles, vendored deps, pure renames),
  and reserve the top tier for a cluster on a risky surface (auth, crypto,
  concurrency, raw SQL or shell, money). Verify every returned finding yourself
  before reporting it. Never truncate, and never report partial coverage as
  complete. Then run one integration pass over the combined findings for what
  no single cluster sees alone — an export removed in one, its only caller
  in another (`break:`/`contract:`).

{{lenses}}

## Don't repeat what's already on the PR

Before you report, read what's already there — re-posting a finding another
reviewer already made is how a review loses trust. Fetch the existing comments
(bots like CodeRabbit / CodeAnts post here too):

- `gh api repos/{owner}/{repo}/pulls/<N>/comments` — inline review threads
- `gh api repos/{owner}/{repo}/issues/<N>/comments` — the PR conversation
- `gh api repos/{owner}/{repo}/pulls/<N>/reviews` — summary bodies, incl.
  deadeye's own prior run

Drop anything already raised — match on the sink or the fix, not exact wording
(you and a bot word the same bug differently). Report only net-new, and print
one honest line so coverage stays clear —
`N findings already raised by existing reviewers — skipped` — whether you're
posting or just printing.

## Learning loop (repo-scoped priority)

Before finalizing, run `deadeye lessons priority` (best-effort — if
`deadeye` isn't on PATH, retry once with `~/.deadeye/bin/deadeye`; if that
also fails, review normally). It prints this repo's recent signal, if any:

- **Recent coder misses** — scrutinize those lens/tags harder; a shape that
  slipped through before is worth a second look.
- **Recently disputed findings** — need stronger `proof:` before reporting
  that lens/tag again. Never skip it outright: one dismissal doesn't retire
  a whole tag, it only raises the bar for the next one.

When the user disputes a finding you reported ("that's not a bug",
"already handled", "won't fix"), record it so the next review on this repo
weighs that lens/tag accordingly:

```bash
deadeye lessons record review-false-positive <lens>:<tag>
```

using the lens the finding came from (`over-engineering`, `correctness`,
`performance`, or `security`) and its tag without the trailing colon —
e.g. a disputed `race:` finding → `deadeye lessons record review-false-positive correctness:race`.

**Catch what you missed.** Among the other reviewers' comments you already
fetched above (for dedup), some may be a real, concrete finding you did NOT
report yourself — a human or another bot catching something your own pass
missed. For each one that reads like a genuine bug or exposure, not a style
preference, a question, or unrelated feedback: verify it the same way you'd
verify your own finding — trace it in the actual diff, don't just trust the
claim (a review comment is a claim, not a work order). If it holds up and
you didn't already report it, record it under the lens/tag it belongs to:

```bash
deadeye lessons record external-miss <lens>:<tag>
```

## Activity tracking (for the report)

Separately from the learning loop above, `deadeye report` builds a local
status page from raw review activity — how many PRs got reviewed, how many
findings, how many actually posted. Same best-effort contract as every
other write-back here (if `deadeye` isn't on PATH, retry once with
`~/.deadeye/bin/deadeye`; if that also fails, keep going — this never gates
the review):

- **Once per run, always**, regardless of what you find:
  `deadeye report record reviewed`
- **Once per finding that survives to the final report** (never a raw
  candidate, never one already raised by another reviewer):
  `deadeye report record finding <lens>:<severity>` — the lens
  (`over-engineering`, `correctness`, `performance`, `security`) and the
  finding's severity word (`critical`, `high`, `medium`, `nit`, matching its
  glyph), e.g. `deadeye report record finding security:critical`.
- **Once per finding dropped in the "Don't repeat" pass above**:
  `deadeye report record skipped`
- **Once per finding actually included in a `--post`ed review** (see
  "Posting back to the PR" below) — only after the post succeeds, never for
  a print-only run: `deadeye report record posted`

## Output

Lead with a one-line header, then the four lens sections, then a verdict:

```
PR #<N> "<title>"  +<adds>/-<dels>, <files> files
```

End with the tally and the verdict — `<C> critical, <H> high, <M> medium, <N>
nits` and the one `critical` that must ship fixed — or, when nothing
survived verification, exactly: `Clean — nothing survived verification.
Ship it.`

Findings are a LIST. Do not apply or push any code change unless asked.

{{fixes}}

## Posting back to the PR (opt-in only)

Default is print-only — nothing is sent anywhere. Post the review to GitHub
ONLY when the user passes `--post` or explicitly asks:

- A suggested-fix snippet (see "Suggested fixes" above) becomes the comment's
  fix content as a `` ```suggestion `` block instead of a plain fenced one,
  anchored to the exact lines the diff shows — GitHub renders a
  one-click "Apply suggestion" button, the fastest path from finding to fix.
  A suggestion block can only replace lines already in the diff; if the fix
  reaches outside them, post the plain snippet and prose fix instead —
  GitHub rejects a suggestion that doesn't fit the anchored range.
- Show the exact comment body first and get an explicit yes — posting is
  outward-facing and public on the PR.
- **Redact any secret value** a `secret:`/`expose:` finding surfaced before it
  goes into a public comment — name the location, never the credential.
- Post ONE review that anchors each finding to its line — not a wall of text
  in a single comment. `gh pr review` only posts a summary body, so use the
  API for inline anchors: build a JSON payload and
  `gh api repos/{owner}/{repo}/pulls/<N>/reviews --input -` with
  - `event: "COMMENT"`,
  - `body`: the tally + verdict (the summary),
  - `comments`: one entry per finding, `{path, line, side, body}` —
    `side: "RIGHT"` for an added/context line, or `side: "LEFT"` with the
    ORIGINAL file's line number for a finding on a deleted line (a removed
    guard, a dropped `ok &&`) — the body being the finding line (severity,
    tag, fix, proof).
  Get `{owner}/{repo}` from `gh repo view --json nameWithOwner`. Anchor `line`
  to a line the diff actually touches, or GitHub rejects the comment.
- `event: "COMMENT"` only — never approve, request-changes, merge, or close.
