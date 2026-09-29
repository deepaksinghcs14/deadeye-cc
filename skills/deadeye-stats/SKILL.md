---
name: deadeye-stats
description: deadeye's own measurements in one place -- measured impact, token savings, per-session context breakdown, and whether its reviewer, router and coder persona hold up against what actually happened.
license: MIT
argument-hint: "[savings|context|impact|accuracy|disagreement|adherence|laya] [session-id]"
---

# Deadeye Stats

One entry point for every report deadeye computes about itself. Pick a view
by the first argument; with none, show the measured-impact scoreboard.

Economics, from the decision log (`~/.deadeye/decisions.jsonl`):

- no arg, or `impact` → the measured scoreboard (`deadeye gain`)
- `savings` → the full token-savings report (`deadeye audit`)
- `context [session-id]` → per-session context-byte breakdown (`deadeye context [session-id]`)

Judgment, from review/coder receipts (`~/.deadeye/receipts.jsonl`) and the
outcomes store — deadeye checked against what actually happened:

- `accuracy` → candidate review misses and recorded disputes (`deadeye misses`)
- `disagreement` → where the judge would have routed cheaper (`deadeye disagreement`)
- `adherence` → the coder ladder's checkable rungs on shipped diffs (`deadeye adherence`)
- `laya` → agreement between the optional local Laya classifier and what deadeye actually did (`deadeye laya agreement`)

The three judgment views measure GOING FORWARD only: they read receipts
written when a review or coder session runs, and nothing before this
feature existed left one. An empty report means "not measured yet", never
"nothing wrong" — say which.

`deadeye gain`'s last line is a `file://` link to a regenerated visual
report (`deadeye report` under the hood) — relay it as-is, the same as
every other figure in this skill.

Run the matching binary and present its output **as-is** — every figure
comes from the decision log, the receipts log, or the repo's own git
history, not an invented aggregate. If the binary
reports "command not found", it's almost certainly just not on PATH
(deadeye never adds itself to PATH; it resolves its own binary internally
for hooks). Retry the self-bootstrap path directly, e.g.
`~/.deadeye/bin/deadeye gain`. Only if that also fails is it genuinely not
bootstrapped yet (it self-installs on the first hook invocation).

If the log is empty, the binary prints its own explanation — relay it as-is
and suggest running a task with the plugin active first.

## Honesty boundaries (load-bearing — do not soften)

These carry over from the three reports this skill replaces. Keep them
exactly when presenting any view:

**impact (`deadeye gain`)**
- NEVER print a per-repo savings percentage for code that was never
  written — the unbuilt version has no baseline to subtract from.
- Estimates and measurements are labeled differently in the output; keep
  that distinction.
- For per-repo reality, point at `/deadeye-debt` (shortcuts actually taken)
  and `/deadeye-review --repo` (what's still cuttable, or risky).

**savings (`deadeye audit`)**
- Decisions per surface and per action are counts from the log.
- Preprocessing rewrite figures are per-rule **estimates** (a typical-case
  constant — PreToolUse runs before the command does, so the real output
  size isn't known yet). Present them as estimates, not measurements.
- The trailing "Real Claude Code usage" block is **measured**, not
  estimated — read straight from this project's own Claude Code session
  transcripts (the same numbers `/usage` renders), not from deadeye's
  decision log. Keep its scope caveat when relaying it: it's scoped to the
  CURRENT PROJECT, while every other figure in this report is global
  across every project deadeye has ever run in on this machine — a real
  number on a different scope, not a direct reconciliation. If it's
  missing (best-effort: Claude Code's transcript format/layout is
  undocumented), fall back to suggesting a manual `/usage` check.

**accuracy (`deadeye misses`)**
- Every candidate is `likely`, NEVER confirmed. A fix-shaped commit
  touching lines a review passed is evidence, not proof — the fix may be
  new work, a refactor, or a bug the reviewed diff never contained.
- Nothing is recorded automatically. If a candidate is genuinely a miss,
  the user records it (`deadeye lessons record external-miss <lens>:<tag>`);
  never present the report's count as a confirmed miss count.
- Reviewed work that hasn't been committed yet is excluded from the
  denominator, not counted as a clean pass. Keep that distinction when
  relaying the numbers.
- Both error directions belong together: candidate misses (false
  negatives) AND recorded disputes (false positives). Never relay one
  alone as "accuracy".

**disagreement (`deadeye disagreement`)**
- This is the judge's OPINION, not a measured over-route rate. Never call
  it one. A cheaper tier might still have failed the task.
- The reason it's an opinion: an arbitrary production subtask has no
  grader, so the work can't be replayed and scored. Graded comparison
  lives in `benchmarks/routing/`, on fixtures that ship hidden tests.
- Sampling is OFF by default and costs a real `claude -p` call per sample.
  If it's off, the report says so — relay that rather than reporting zero
  disagreements as a clean result.
- It changes no routing behavior: escalation bias stays one-directional
  until the number has been trusted for a while.

**adherence (`deadeye adherence`)**
- Measures only the mechanically checkable ladder rungs on diffs that
  SHIPPED: rung 5 (a new dependency) and rung 7 (files/insertions per
  commit). These are signals, not verdicts — a wide commit can be the
  right shape, and a new dependency can be exactly what rung 5 asks for.
- Never present it as full ladder coverage. Rung 2 (already in this
  codebase) is unmeasured — it needs a function-level symbol index this
  plugin doesn't build. Rungs 1, 3, 4 and 6 are judgment calls.
- Never turn it into a lines-not-written saving. Same boundary as
  `impact`: the unbuilt version has no baseline to subtract from.
- Attribution is by time window (12h after a recorded coder session), not
  proof — git records no persona, so a commit written with coder mode off
  inside that window still counts. Say so if the figures are load-bearing.

**laya (`deadeye laya agreement`)**
- It is an AGREEMENT rate, never an accuracy rate. On the `shadow` and
  `advise` rungs "actual" is whatever the existing mechanism chose, which is
  itself not ground truth — two classifiers agreeing means they agree.
- Always carry the untuned figure when the user is deciding whether to
  promote: Laya's accuracy on typed decisions is 0.362 on its vendor's own
  eval against ~0.33 for a three-way guess, and every published number for
  it is vendor-self-reported with no independent evaluation found.
- If `mode.laya` is `off`, say so — an empty report means "not measured",
  not "the classifier agrees". Point at `/deadeye-laya` for setup.
- Never imply deadeye ships, installs, or supervises Laya. The user runs
  `laya-serve`; deadeye only talks to an endpoint and falls back when it
  isn't answering.

**context (`deadeye context`)**
- "Injected by deadeye" figures are real byte measurements taken at
  injection time.
- "Observed arrivals" is a FLOOR, not a session total — only outliers are
  logged (built-in tool responses over 8KB, every MCP response), so the
  true arrival total is higher.
- "Kept out of context" keeps the estimated/measured split — never blend a
  per-rule estimate with a measured filtered size.
- No id shows the newest session; pass a session id for an older one (an
  unknown id lists the newest five to pick from).

One-shot: do NOT change the coder level or persist anything else. The one
exception is `report.html`, regenerated as a side effect of `deadeye gain` --
a disposable cache artifact rebuilt from the same decision log every time
(the same doctrine `internal/codemap`'s cache already follows), not
persisted session state.
