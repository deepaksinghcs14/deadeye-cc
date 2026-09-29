---
name: deadeye-laya
description: Set up, verify, and tune the optional local Laya decision model for deadeye -- install laya-serve, point deadeye at it, and promote it up the trust ladder on evidence.
license: MIT
argument-hint: "[status|install|verify|promote|demote|off]"
---

# Deadeye Laya

Set up the optional **Laya** decision model (github.com/NandhaKishorM/laya,
Apache 2.0) so deadeye can answer typed decisions with a local classifier
instead of a `claude -p` call.

**deadeye never installs, bundles, or supervises Laya.** Laya is Python;
deadeye is a single static Go binary with no runtime dependencies. The whole
contract is an HTTP endpoint: the user runs `laya-serve`, deadeye points at
it, and everything falls back to today's behavior the moment it isn't
answering. Say this plainly if the user expects deadeye to install it.

Pick a step from the first argument; with none, run `status` and recommend
the next step.

- `status` → `deadeye laya status` + `deadeye laya agreement`
- `install` → walk the prerequisites and the install, then verify
- `verify` → `deadeye laya health` then `deadeye laya test`
- `promote` / `demote` → move one rung on the ladder, with the evidence check below
- `off` → turn it off; see **Turning it off** below for all four ways

Every `deadeye` invocation here is best-effort the same way the other
skills' are: if it reports "command not found", retry once with
`~/.deadeye/bin/deadeye` before concluding anything.

**If `deadeye laya ...` reports `unknown command "laya"`, the BINARY is
older than the plugin.** Updating the plugin refreshes these instructions
but not the compiled binary, which self-installs on a hook invocation and
can lag by a release. Check with `deadeye version`; if it's behind, either
start a new Claude Code session (the SessionStart hook re-bootstraps it) or
run the plugin's own `hooks/bootstrap.sh` directly. Don't work around it —
every command below lives in that binary.

## The ladder (this is the whole design)

`mode.laya` is four rungs, not a switch:

| Rung | Laya is asked | Answers recorded | Answers acted on |
|---|---|---|---|
| `off` (default) | no | — | — |
| `shadow` | yes | yes | **no** |
| `advise` | yes | yes | no (shown in decision reasons) |
| `authoritative` | yes | yes | **yes** |

**Never start at `authoritative`, and never promote without looking at the
agreement number.** The reason is specific, not ceremonial: Laya's untuned
accuracy on typed decisions is **0.362 on its vendor's own eval, against
~0.33 for a three-way coin flip**, and no independent evaluation exists.
Shadow mode exists so the user finds that out on their own traffic, at zero
risk, before anything routes on it.

## install

1. **Check prerequisites.** Laya needs **Python 3.10+**, and `python3` is
   often older than that — macOS still ships 3.9. Check what's actually
   there before building anything:
   ```bash
   python3 --version
   for p in python3.13 python3.12 python3.11 python3.10; do command -v $p; done
   ```
   Use the newest one you find, and use it BY NAME in the next step: a venv
   built with a 3.9 `python3` fails at install with an unhelpful resolver
   error rather than saying "wrong Python". If nothing 3.10+ exists, stop
   and say so — installing a Python is the user's call, not this skill's.

   Tell the user the real cost before they commit: the install pulls
   **torch, transformers and their dependencies (multiple GB)**, then
   **~843MB** of weights per checkpoint on first load. On CPU, inference
   runs **193-464ms** per call with the model resident (the widely-quoted
   32.8ms is a T4 GPU figure), and a cold checkpoint load costs several
   seconds.
2. **Install into a venv, never system Python** (substituting the
   interpreter found above for `python3.12`):
   ```bash
   python3.12 -m venv ~/.deadeye/laya-venv
   ~/.deadeye/laya-venv/bin/pip install "laya[serve]"
   ```
   Check the plan first with `pip install --dry-run "laya[serve]"` if the
   user wants to see the size before committing.
3. **Serve the `typed-decisions` checkpoint.** This is the step that
   matters, and the easiest one to get wrong:
   ```bash
   LAYA_MODELS=typed-decisions LAYA_PRELOAD=1 ~/.deadeye/laya-venv/bin/laya-serve
   ```
   Laya ships three checkpoints — `english`, `multilingual`, and
   `typed-decisions` — and **the server's router only chooses between the
   first two**, by script and language. It never reaches `typed-decisions`
   on its own unless started with `LAYA_AUTO_TASK=1`. Every question deadeye
   asks (tier choice, plan-needed, workflow-shaped, is-this-a-bug-fix,
   complexity) is a typed decision, and upstream's own benchmark puts the
   base checkpoint at **0.362** against **0.766** for `typed-decisions` —
   "all of the capability on this benchmark comes from fine-tuning". Serving
   the base weights means asking the wrong model.

   deadeye names the checkpoint on every request (`laya.checkpoint`,
   default `typed-decisions`), so it doesn't depend on the server's routing
   — but the server still has to have that checkpoint available, which is
   what `LAYA_MODELS` above does.

   **Check the port is free first** (`lsof -nP -iTCP:8000 -sTCP:LISTEN`).
   8000 is a busy default — a dev server already sitting there will answer
   `deadeye laya health` with a 404 and look like a broken Laya, or worse
   answer 200 and look like a working one. Use `LAYA_PORT=8791` (or any
   free port) and set `laya.endpoint` to match.

   It binds `0.0.0.0:8000` by default. It must keep running — tell the user to leave it
   in its own terminal, or set it up under `launchd`/`systemd` themselves.
   Other env vars worth naming: `LAYA_DEVICE=cuda` if they have a GPU,
   `LAYA_PORT`, `LAYA_THREADS`, `LAYA_API_KEY` for a bearer token, and
   `LAYA_MAX_LOADED` (default 2) if they want more than two checkpoints
   resident at once.
4. **Point deadeye at it and start in shadow:**
   ```bash
   deadeye config set laya.endpoint http://127.0.0.1:8000
   deadeye config set mode.laya shadow
   ```
   If the server needs a token, the user exports it and deadeye reads the
   variable by NAME (`laya.api_key_env`, default `LAYA_API_KEY`) — never
   write the token into `config.json`, which `deadeye config` prints.

   **Warn the user about this one:** the hook path runs inside deadeye's
   long-lived daemon, which only sees the environment it was *started* with.
   A token exported in their shell afterwards is invisible to it, so every
   hook-path call would get a 401 and silently fall back — while
   `deadeye laya health` and `laya test`, which run in their shell, both
   report green. If `laya-serve` needs a token, it has to be exported before
   the daemon starts (or just run `laya-serve` without one on loopback,
   which is the simpler setup).
5. **Verify** (below). Then stop: shadow is the correct resting state until
   there's data.

## verify

```bash
deadeye laya health
deadeye laya test
```

`health` is reachability only. `test` sends one real tier question about an
obviously mechanical task (a variable rename) and prints the tier, the
certainty, the latency, and **which checkpoint answered**.

Interpret it honestly for the user:

- **Checkpoint is not `typed-decisions`** → the most important thing to
  catch. The server is serving the wrong weights; restart it with
  `LAYA_MODELS=typed-decisions`. Do not recommend promoting past `shadow`
  until this is right — the numbers collected on base weights say nothing
  about the fine-tuned ones.
- **Tier 0 returned** → working and plausible on this task.
- **Tier 1 or 2 returned** → the endpoint works but the classifier is wrong
  on an easy case. Say so directly and leave `mode.laya` at `shadow`.
- **Slow first call** → expected, that's the cold load; run it again.
- **Reachable but no usable answer** → either the checkpoint hasn't finished
  loading, or `laya.endpoint` points at something that isn't laya-serve.
  Check what owns that port before assuming the model is at fault.

## A caveat about certainty

`laya-serve` logs this at startup for the `typed-decisions` checkpoint:

> this checkpoint ships invalid temperatures or values outside [0.5, 5] …
> Treat confidence from the affected entries as uncalibrated.

That matters here because deadeye gates on certainty in three places: the
complexity signal's floor, the gate-confirmation bar, and the "no certainty
means no answer" check. Uncalibrated confidence doesn't make those unsafe —
they all fail toward doing nothing — but it does mean a high
`answer_confidence` is not evidence of much. Mention it if a user starts
reasoning from the certainty figures.

## promote / demote

Before promoting, always run:

```bash
deadeye laya agreement
```

It reports agreement per call site over 30 days. Read it out with its own
caveat intact: **this is agreement, not accuracy.** On `shadow` and
`advise`, "actual" is whatever the existing mechanism chose, which is not
ground truth — two classifiers agreeing means they agree.

Guidance for the recommendation:

- **Fewer than ~50 verdicts** → not enough to judge. Stay put, keep
  collecting.
- **Any site below ~60%** → do not promote. Name the site.
- **Consistently high across sites, with real volume** → promoting to
  `advise` is reasonable; `authoritative` is the user's call and should come
  with the reminder that it starts changing routing, gates, and the
  `deadeye misses` classifier.

Demote on any sign of trouble — it costs nothing and loses no data:

```bash
deadeye config set mode.laya shadow
```

## Turning it off

**Laya can always be turned off, instantly, and nothing is lost.** Recorded
verdicts stay where they are, and deadeye goes back to behaving exactly as it
did before Laya existed. There are four ways, and it's worth telling the user
which one fits:

1. **Back down the ladder** — the usual answer. Keeps collecting evidence
   while nothing acts on it:
   ```bash
   deadeye config set mode.laya shadow
   ```
2. **Off entirely** — stops calling Laya at all:
   ```bash
   deadeye config set mode.laya off
   ```
3. **Right now, without editing config** — an env kill switch, same family as
   `DEADEYE_PREPROCESS=off` / `DEADEYE_CODER=off`. Good for one command or one
   shell, and it wins over whatever `config.json` says:
   ```bash
   DEADEYE_LAYA=off claude      # this session only
   export DEADEYE_LAYA=off      # this shell
   ```
   `DEADEYE=off` turns off everything including Laya.
4. **Stop `laya-serve`** — deadeye fails open on an unreachable endpoint, so
   killing the server disables every call site with no config change at all.
   This is the one that needs saying out loud: a dead endpoint and a disabled
   Laya are *indistinguishable in behavior* by design. That's why
   `deadeye doctor` carries a `laya` row — otherwise "it stopped working" and
   "it's off" look identical.

Clearing the endpoint (`deadeye config set laya.endpoint ""`) also disables
it, whatever `mode.laya` says.

## What waits, and what doesn't

Only the rungs that act on an answer wait for it:

- **`shadow`** — nothing waits. Routing and both gate checks ask Laya off the
  critical path and record the answer when it arrives. A measurement rung that
  slowed the user's own turn would be indefensible.
- **`advise`** — the routing question waits (its answer is printed in the reason
  the user reads); the two gate checks still don't.
- **`authoritative`** — waits wherever the answer decides something.

Say this if a user worries about latency on `shadow`: there isn't any.

## Keep the endpoint on loopback

Nothing forbids pointing `laya.endpoint` at another host, but then every task
description deadeye classifies leaves the machine — which would quietly
falsify the "nothing leaves your machine" claim this feature is sold on.
`deadeye laya status` and `deadeye doctor` both warn when the endpoint isn't
loopback. What gets sent is the task description, or for the plan-gate and
workflow-hint checks the user's prompt as typed — so it contains whatever
they put in it.

## Where Laya gets used (only when configured)

Six sites, all opt-in, all fail-open:

- **Routing judge** — the headline. On `authoritative` a local tier answer
  replaces the `claude -p` judge call, which is the only thing in deadeye
  that blocks a hook response on a model call.
- **Complexity signal** — an optional seventh signal beside the six cheap
  heuristics. Added only on `authoritative`; skips quietly below a certainty
  floor so a hedging classifier can never drag a decision's confidence down.
- **Plan gate** and **workflow hint** — Laya can only CONFIRM or SUPPRESS a
  gate the heuristic already fired; it never fires one itself. Suppression
  is the direction where being wrong is cheap.
- **Tier-sample screen** — a free local screen on every confident high-tier
  route, escalating only disagreements to a `claude -p` confirmation. This
  is what makes `/deadeye-stats disagreement` dense instead of a 1-in-10
  sample.
- **`deadeye misses` commit classifier** — replaces a `fix|bugfix|revert`
  regex with "does this commit message describe fixing a bug?", catching the
  fixes that never say "fix". Offline, so latency is irrelevant here — the
  lowest-risk site of the six.

## Honesty boundaries (load-bearing — do not soften)

- Never present Laya as bundled with deadeye, or imply deadeye manages its
  lifecycle. The user owns that process.
- Never quote 32.8ms as the expected latency without saying it's a T4 GPU
  number; CPU is 193-464ms resident.
- Never say Laya slows things down on `shadow` -- it doesn't, it's off the
  critical path there. And never imply a remote endpoint is equivalent to a
  local one; it isn't, and doctor says so.
- Never call the agreement number an accuracy number.
- Always state the 0.362-vs-0.33 untuned figure when recommending a
  promotion past `shadow`, and that every published number for Laya is
  vendor-self-reported with no independent evaluation found.
- Always tell the user how to turn it off when you turn it on. Four ways,
  above; the env switch is the one to reach for in a hurry.
- If the user asks for `authoritative` immediately, set it if they insist —
  it's their machine — but tell them once what shadow would have told them
  first, and don't repeat it afterwards.
- Never let a user promote past `shadow` while `deadeye laya test` reports
  a checkpoint other than `typed-decisions`. Agreement collected on base
  weights does not transfer.
- Laya is 11 days old as of deadeye 0.66.3, with ~3 releases a day. Treat
  breaking changes upstream as likely, not hypothetical.
