---
name: deadeye-sweep
description: Applies the findings from /deadeye-review, /deadeye-pr, and /deadeye-guard, verifies the build, and re-scans on a loop until nothing (critical/high, or every severity with --all) survives. In --pr mode also answers the PR's own open review comments, replying and resolving.
license: MIT
argument-hint: "[--repo|--pr [<PR>]] [<max-passes>] [--commit] [--all]"
---

# Deadeye Sweep

`/deadeye-review`, `/deadeye-pr`, and `/deadeye-guard` all end the same way:
"Findings are a LIST. Do not apply them unless asked." Typing `/deadeye-sweep`
IS that ask. It runs the review, audits each finding's premise, applies the
ones that are 🔴/🟠 **and** in reach, proves the tree still builds, then
re-scans the changed code and repeats until a pass comes back clean.

Sweep does not reimplement scanning — it invokes the existing skills and
consumes their printed findings. Coder mode stays active throughout: the
fixes it writes follow the lean-first ladder, not a patch bolted at the line
the finding named.

## Scope

- **default** — the working diff. Scan: `/deadeye-review`, then `/deadeye-guard`.
- **`--pr [<PR>]`** — that PR, or the current branch's PR with none given.
  Scan: `/deadeye-pr <PR>`, then `/deadeye-guard`, then the PR's own open
  review comment threads (see "PR review comments" below). If the current
  branch isn't the PR's head branch, ask before `gh pr checkout <PR>` —
  before pass 1 does anything, since every fix that follows is applied
  locally to that branch.
- **`--repo`** — the whole repository. Scan: `/deadeye-review --repo` only —
  `/deadeye-guard` has no whole-repo mode, and `--repo` already runs the
  security lens plus native dependency auditors.
- A bare integer lowers the pass cap (default: loop until clean, hard cap
  **5**) — e.g. `/deadeye-sweep --repo 2`. The integer right after `--pr` is
  the PR number; any other bare integer is the cap.
- `--commit` — commit each pass that verifies green. Off by default. Never
  pushes on its own; see "PR handoff" below.
- `--all` — lowers the severity floor to include 🟡 medium and ⚪ nits (see
  "What gets fixed"). Off by default — the unattended default stays 🔴/🟠
  only. The reach gate still applies either way; `--all` widens which
  severities qualify, not how far a fix is allowed to reach.

Empty diff (nothing changed or staged): say so plainly and stop — do not
substitute a different scope. Not a git repo: stop, sweep needs a restore
point it can't build without one.

**The scan's verdict is step 2 of 6, not the end.** `/deadeye-review` and
`/deadeye-pr` close with something that reads like a finish line
(`Ship it.`). When you invoke them here, that verdict is not the end of the
turn — carry the findings straight into the premise audit below.

## The loop

One pass:

1. **Scan.** Pass 1 invokes the review skill for the scope above, then (diff
   and PR modes only) `/deadeye-guard` — sequentially, never both loaded at
   once. Pass 2+ does **not** re-invoke the Skill tool: the rubric is already
   in context, and re-running the scan procedure directly against the current
   diff is the whole point of the loop. Pass 2+ scope:
   - default / `--pr`: full re-scan — `git diff` (default) or
     `git diff <baseRefName>...HEAD` locally for `--pr` (not `gh pr diff`,
     which reads the *remote* branch and would just re-report what pass 1
     already fixed locally).
   - `--repo`: run the diff rubric (not `--repo` again) over the accumulated
     working diff, plus an explicit grep for callers of anything the previous
     pass deleted or renamed. Reuse the diff scan, don't hand-roll a third
     scan mode.

   In `--pr` mode only, pass 1 additionally pulls every open review comment
   thread on the PR and folds each into this same pass's triage — see "PR
   review comments" below for the fetch, the (absent) floor, and how they get
   answered.
2. **Audit the premise.** A finding is a claim, not a work order — re-grep
   what it asserts (callers, implementers, existing guards, the absence it
   claims) before touching anything. Disproved → drop it, print the one-line
   evidence, and move on. Fixing fewer findings than the scan printed is
   correct behavior here, not a shortfall. Do not record a disproved finding
   as `review-false-positive` — that record means a *human* disputed it
   (see Learning loop); auto-recording it here would let sweep desensitize
   its own scanner with nobody in the loop.
3. **Triage.** Split survivors by both gates in "What gets fixed."
4. **Apply.** You write every fix yourself, in the main session, coder mode
   active — no subagent fan-out. The severity floor already bounds a pass to
   a handful of findings; splitting them across subagents would separate a
   finding from the cross-file context that made it real (an interface named
   in one file, its one implementer in another), make a red build
   unattributable to one edit, and lose coder mode's session-level persona,
   which a fanned-out agent does not inherit.
5. **Verify.** Run the check command (see gate below). Green → pass recorded.
   Red from formatting alone → run the formatter, re-check, no repair budget
   spent. Red from anything else → one repair attempt, then restore the
   snapshot and stop.
6. **Re-scan.** Loop back to step 1 for the next pass.

**Confirm once, before pass 1, then run unattended.** Print the triaged list
— what will be fixed, what's deferred and why, what's below the active floor
(and that `--all` is on, if it is), which check command will run, whether
`--commit` is on and on which branch (flag it if it's the default branch),
and (in `--pr` mode) how many open review comment threads were found and how
many are in scope — and wait for a go-ahead. Say plainly that
passes 2+ will **not** stop again, bounded only by the gates below — except:
if any later pass's fix list is more than 2× pass 1's, stop and re-confirm.
An unattended writer suddenly going wide is exactly what that check catches.

## What gets fixed

Both gates must pass:

- **Severity floor: 🔴 critical and 🟠 high by default.** 🟡 medium and ⚪ nits
  are printed in the final report, never applied — churn, not consequence, in
  an unattended run. With `--all`, all four severities clear this gate; state
  that explicitly at the confirmation step, since it changes what an
  unattended pass is allowed to touch.
- **Reach: the fix is contained in the files the finding names.** Unaffected
  by `--all` — a nit still doesn't buy a signature change into untouched
  callers.

Deferred — reported, not touched:

- a fix that changes an exported signature and ripples into call sites this
  work never entered
- a design decision with more than one defensible answer
- vendored code, generated code, lockfiles

Coder mode's own rule, applied to someone else's finding list: fix what's in
reach, name the rest.

Dependency findings from the guard pass are in reach when the fix is a bump
to a fixed version — edit the manifest and regenerate the lockfile with the
native tool (`go get`, `npm install`), never hand-edit a lockfile. A
major-version bump, or a CVE with no fixed version, is a design decision →
defer.

Never delete the one runnable check coder mode leaves behind, or the one log
breadcrumb at a real failure boundary — both are load-bearing, same as in
`/deadeye-review`.

## The verify gate

First check command that applies, in order: `make check` (or `make test` if
that's what the Makefile defines) → Go: `go build ./... && go test ./...` →
Node: the `package.json` test script, plus `tsc --noEmit` if a tsconfig
exists → `pytest` / `cargo test`. None found → ask once, at the confirmation
step; declined → build only, and say so in the output. Never print a green
verdict for a check that didn't run.

Restore point, taken before applying, every pass:

```bash
git add -N <untracked paths in scope>   # so they're visible to the scan and the stash
git stash create
git rev-parse HEAD                       # fallback — stash create prints nothing on a clean tree
```

A red verify after the one repair attempt: `git checkout <sha> -- .`, stop,
print the failure output.

```
deadeye: snapshot restores tracked files only. ceiling: files a pass newly
creates survive the revert. upgrade: when sweep starts creating files.
```

With `--commit`: stage only the paths this pass edited —
`git add <paths>` then `git commit -m "sweep: pass <n> — <k> fixes"`. Never
`git commit -a`: in the default scope, the user's own uncommitted work *is*
the scope, and `-a` would fold it into a commit labeled as sweep's. Never
`git push`.

## Stop conditions

First one that fires wins:

1. A scan returns nothing at or above the active floor (🔴/🟠, or all four
   under `--all`) → converged
2. Pass cap reached (the user's N, else 5)
3. Verify red after the one repair attempt → restore, stop
4. A pass applies zero fixes (everything dropped or deferred) → no progress, stop
5. Oscillation — a finding at the same `path + tag + normalized description`
   (never line number, which shifts every edit) reappears after being fixed →
   stop and report it
6. A pass's fix list exceeds 2× pass 1's → stop and re-confirm

## Learning loop

Same mechanism as `/deadeye-review`, same best-effort contract (retry once
with `~/.deadeye/bin/deadeye`, else continue regardless). For every finding
you actually **applied** this run:

```bash
deadeye lessons record coder-miss <lens>:<tag>
```

Diff and PR scope only — `--repo` sweeps pre-existing code nothing wrote this
session, so it never attributes to coder mode. Never call
`lessons record review-false-positive` from here — that record means a human
disputed the finding; a premise-audit drop is sweep's own judgment, not
theirs, and auto-recording it would quietly desensitize the next scan.

## Output

```
pass 1 — 11 findings · 2 dropped (premise) · 4 🔴/🟠 in reach · 4 fixed · check: green
pass 2 — 3 findings · 1 fixed · check: green
pass 3 — clean

Converged in 3 passes. 5 fixes, +11/-84. Tree is dirty — review with `git diff`.
Dropped (2): internal/x.go:41 — authz: guard exists one call down at y.go:88.
Deferred (1): internal/x.go:12 — yagni: exported, 6 call sites outside this change.
Below floor: 3 🟡, 1 ⚪.
Say the word and I'll take the deferred one and the mediums.
```

(Under `--all` there's no "Below floor" line — every severity already cleared
the gate, so what's left is only "Dropped" and "Deferred".)

Terminal strings: `Converged — nothing left to cut.` when a pass finds
nothing left; `Nothing to sweep — the scan came back clean.` when pass 1
itself finds nothing (distinct from convergence — say plainly there was no
work to do). No `DEADEYE-SWEEP.md`, no state file between runs — leftovers
are printed and closed with the one follow-up line, same as `/deadeye-debt`'s
report-only convention.

## PR review comments and handoff (`--pr` mode)

Pass 1 pulls the PR's own review threads and treats each **unresolved** one
as an additional finding, fed through the same pipeline a scanner finding
gets from here on. REST alone can't tell resolved from open — only the
GraphQL thread carries `isResolved` — so fetch via:

```bash
gh api graphql -f query='
  query($owner:String!,$repo:String!,$pr:Int!) {
    repository(owner:$owner,name:$repo) {
      pullRequest(number:$pr) {
        reviewThreads(first:100) {
          nodes { id isResolved
            comments(first:100) { nodes { databaseId body path line author { login } } }
          }
        }
      }
    }
  }' -F owner=<owner> -F repo=<repo> -F pr=<PR>
```

- **No severity floor** — a person asking is already in scope, `--all` or
  not.
- **The reach gate still applies** — a comment asking for a change that
  ripples into untouched callers is deferred exactly like a scanner finding.
- **The premise audit still applies** — "a review comment is a claim, not a
  work order" governs these the same as a scanner finding. A comment can be
  stale (the code moved since it was written) or already handled by an
  earlier pass in this same run; re-grep before treating it as work.
- It enters the normal apply → verify → (revert on red) pipeline, so a
  reverted pass takes its comment-driven fixes down with it too.

**When the loop ends**, resolve every thread that got a real answer this run,
and offer the two outward actions below — each its own explicit
confirmation, all presented together, none implied by the original
invocation:

1. **Reply and resolve, per thread:**
   - *Fixed* → reply describing what changed, then
     `POST /repos/<owner>/<repo>/pulls/comments/<databaseId>/replies` with the
     reply body, then resolve:
     `gh api graphql -f query='mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}' -F id=<thread node id>`.
   - *Premise disproved* (stale, already handled) → reply with the evidence.
     **Leave it open** — closing someone else's thread on your own say-so
     isn't sweep's call.
   - *Deferred* (out of reach, a design decision) → reply saying so and why.
     **Leave it open.**
2. **Push the fixes** to the PR branch.
3. **Post one comment** summarizing what was auto-fixed and what was left —
   same opt-in shape as `/deadeye-pr --post`, not a new authorization path.

Print exactly what each action would do — the reply text included — before
asking.

**Write every reply like the person who made the fix, not a report
generator.** Say what you actually did, plainly, the way you'd tell a
teammate over their shoulder: "Good catch — moved this into gitutil since
both files already had a copy" reads like a person; "Fixed. lens: reuse.
tag: dup. proof: codemap.go:349, sessionmem.go:38." reads like a scanner
dump pasted into a conversation. Keep the `Fix:`/`proof:`/tag glyph shape for
sweep's own local report only — never in a reply a human will read. Address
what the reviewer actually wrote, keep it to a sentence or two, and let the
phrasing vary thread to thread the way a person's actually does — five
replies that all open "Fixed:" read like a bot even if every fix is real.

## Boundaries

- Never push, reply, or resolve a thread without the confirmation above.
- Never resolve a thread whose premise was disproved or whose fix was
  deferred — only one that was actually fixed.
- Never commit without `--commit`.
- Never substitute a different scope than the one requested.
- Leave the tree dirty for the user to review with `git diff` (or committed,
  under `--commit`) — sweep never claims done on your behalf.
- `/deadeye-review` and `/deadeye-pr` stay the report-only versions of this;
  `/deadeye-guard` stays the dedicated security-only pass.
