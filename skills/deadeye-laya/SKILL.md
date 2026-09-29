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

1. **Check prerequisites.** Python 3.10+ (`python3 --version`). Tell the
   user the real cost before they commit: the English checkpoint is
   **~843MB** of weights, and on CPU inference runs **193-464ms** per call
   with the model resident (the widely-quoted 32.8ms is a T4 GPU figure).
   A cold checkpoint load costs several seconds.
2. **Install into a venv, never system Python:**
   ```bash
   python3 -m venv ~/.deadeye/laya-venv
   ~/.deadeye/laya-venv/bin/pip install -q "laya[serve]"
   ```
3. **Start the server, preloaded** so the first real decision doesn't pay
   the cold load:
   ```bash
   LAYA_PRELOAD=1 ~/.deadeye/laya-venv/bin/laya-serve
   ```
   It binds `0.0.0.0:8000`. It must keep running — tell the user to leave it
   in its own terminal, or set it up under `launchd`/`systemd` themselves.
   Other env vars worth naming: `LAYA_DEVICE=cuda` if they have a GPU,
   `LAYA_PORT`, `LAYA_API_KEY` for a bearer token.
4. **Point deadeye at it and start in shadow:**
   ```bash
   deadeye config set laya.endpoint http://127.0.0.1:8000
   deadeye config set mode.laya shadow
   ```
   If the server needs a token, the user exports it and deadeye reads the
   variable by NAME (`laya.api_key_env`, default `LAYA_API_KEY`) — never
   write the token into `config.json`, which `deadeye config` prints.
5. **Verify** (below). Then stop: shadow is the correct resting state until
   there's data.

## verify

```bash
deadeye laya health
deadeye laya test
```

`health` is reachability only. `test` sends one real tier question about an
obviously mechanical task (a variable rename) and prints the tier, the
certainty, and the latency.

Interpret it honestly for the user:

- **Tier 0 returned** → working and plausible on this task.
- **Tier 1 or 2 returned** → the endpoint works but the classifier is wrong
  on an easy case. Say so directly and leave `mode.laya` at `shadow`.
- **Slow first call** → expected, that's the cold load; run it again.
- **Reachable but no usable answer** → the checkpoint probably hasn't
  finished loading. Check the `laya-serve` output.

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
- Never call the agreement number an accuracy number.
- Always state the 0.362-vs-0.33 untuned figure when recommending a
  promotion past `shadow`, and that every published number for Laya is
  vendor-self-reported with no independent evaluation found.
- Always tell the user how to turn it off when you turn it on. Four ways,
  above; the env switch is the one to reach for in a hurry.
- If the user asks for `authoritative` immediately, set it if they insist —
  it's their machine — but tell them once what shadow would have told them
  first, and don't repeat it afterwards.
- Laya is 11 days old as of deadeye 0.66.1, with ~3 releases a day. Treat
  breaking changes upstream as likely, not hypothetical.
