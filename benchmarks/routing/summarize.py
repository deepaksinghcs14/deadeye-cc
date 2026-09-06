#!/usr/bin/env python3
"""Aggregate results.jsonl into results/summary.md.

The honest claim has two halves: cost delta AND quality parity. A saving is only
real on a task where the cheaper tier actually PASSED its hidden test -- routing
to a tier that fails means a re-run on a stronger model, which is a cost, not a
saving. So "routed cost" uses, per task, the CHEAPEST tier that passed (the
oracle deadeye's router aims to approximate); baseline is everything on opus.
"""
import json, os

HERE = os.path.dirname(__file__)
rows = [json.loads(l) for l in open(os.path.join(HERE, "results/results.jsonl")) if l.strip()]

TIERS = ["haiku", "sonnet", "opus"]
by = {(r["task"], r["tier"]): r for r in rows}
tasks = sorted({r["task"] for r in rows})
band = {r["task"]: r["band"] for r in rows}


def cost(task, tier):
    r = by.get((task, tier))
    return (r or {}).get("cost_usd") or 0.0


def passed(task, tier):
    r = by.get((task, tier))
    return bool(r and r.get("pass_"))


def cheapest_passing(task):
    for tier in TIERS:  # haiku, sonnet, opus -- cheapest first
        if passed(task, tier):
            return tier
    return None


def pct_of(num, den):
    return f"{(num / den * 100):.0f}%" if den else "n/a"


def judge_pct(n, total):
    return f"{n}/{total} ({(n / total * 100):.0f}%)" if total else "0/0"


out = []
out.append("# Routing-savings benchmark -- results\n")
out.append("Each task ran on all three tiers in an isolated clean tree, graded by a "
           "hidden test the model never saw. Cost is real billed `total_cost_usd`. "
           "Deadeye was disabled during runs so this measures raw per-tier model cost.\n")

# Per-task table
out.append("## Per-task: cost (pass/fail) by tier\n")
out.append("| Task | Band | haiku | sonnet | opus | Cheapest that passed |")
out.append("|---|---|---|---|---|---|")
for t in tasks:
    cells = []
    for tier in TIERS:
        c = cost(t, tier)
        mark = "PASS" if passed(t, tier) else "FAIL"
        cells.append(f"${c:.3f} {mark}")
    cp = cheapest_passing(t) or "none"
    out.append(f"| {t} | {band[t]} | {cells[0]} | {cells[1]} | {cells[2]} | **{cp}** |")
out.append("")

# Pass-rate by band x tier
out.append("## Pass rate by band x tier\n")
bands = ["mechanical", "standard", "hard"]
out.append("| Band | haiku | sonnet | opus |")
out.append("|---|---|---|---|")
for b in bands:
    bt = [t for t in tasks if band[t] == b]
    if not bt:
        continue
    cells = []
    for tier in TIERS:
        p = sum(passed(t, tier) for t in bt)
        cells.append(f"{p}/{len(bt)}")
    out.append(f"| {b} | {cells[0]} | {cells[1]} | {cells[2]} |")
out.append("")

# Cost roll-up
baseline = sum(cost(t, "opus") for t in tasks)
routed = 0.0
reran = []
for t in tasks:
    cp = cheapest_passing(t)
    if cp is None:
        routed += cost(t, "opus")  # even opus failed: harness/task issue, charge opus
        reran.append((t, "none-passed"))
    else:
        routed += cost(t, cp)
saved = baseline - routed
pct = (saved / baseline * 100) if baseline else 0.0

out.append("## Cost roll-up (oracle routing = cheapest tier that passed)\n")
out.append(f"- Baseline (every task on opus): **${baseline:.3f}**")
out.append(f"- Routed (cheapest passing tier per task): **${routed:.3f}**")
out.append(f"- Saved: **${saved:.3f}**  (**{pct:.0f}%**)\n")

# Honesty notes
out.append("## Honesty notes\n")
downshifted = [t for t in tasks if cheapest_passing(t) in ("haiku", "sonnet")]
out.append(f"- {len(downshifted)}/{len(tasks)} tasks were solvable below opus "
           f"(where the saving is real -- the cheaper tier actually passed).")
opus_only = [t for t in tasks if cheapest_passing(t) == "opus"]
if opus_only:
    out.append(f"- {len(opus_only)} task(s) needed opus (cheaper tiers failed the hidden test): "
               f"{', '.join(opus_only)}. Routing correctly keeps these on opus.")
none = [t for t, _ in reran]
if none:
    out.append(f"- {len(none)} task(s) failed on EVERY tier: {', '.join(none)}. "
               f"Charged at opus in the oracle arm, so it claims no savings. The hidden "
               f"test itself was re-validated against an independently written correct "
               f"implementation and passes, so these are genuine model failures, not a "
               f"broken fixture. (An earlier single-trial run's note claimed opus passed "
               f"h5-expr on a manual re-run; two full sweeps have since failed it on all "
               f"three tiers, so that claim is retired rather than repeated.)")
out.append("- Savings above are the ORACLE ceiling (perfect tier choice), not what "
           "deadeye achieves -- see the router arm below for the realized number. "
           "Cache-heavy Claude Code system-prompt cost is included in every run and is "
           "similar across tiers, so it dilutes the headline %; the model-priced delta "
           "is the real lever.")

# ---------------------------------------------------------------- router arm
# The oracle above is hindsight. This section answers the question that
# actually matters: what does deadeye's REAL router pick, and what does that
# cost? Written by router.sh; absent until it's been run.
router_path = os.path.join(HERE, "results/router.jsonl")
rrows = []
if os.path.exists(router_path):
    rrows = [json.loads(l) for l in open(router_path) if l.strip()]

if rrows:
    # Majority vote across trials, so one fail-open doesn't decide a task.
    # A tie (no strict majority) is itself a finding -- reported, not hidden.
    picks, unstable = {}, []
    for t in tasks:
        votes = [r["tier"] for r in rrows if r["task"] == t and r["tier"]]
        if not votes:
            continue
        top = max(set(votes), key=votes.count)
        picks[t] = top
        if votes.count(top) < len(votes):
            unstable.append((t, votes))

    judged = sum(1 for r in rrows if r.get("judge"))
    scored = [t for t in tasks if t in picks]

    def realized(task):
        """Cost of actually following deadeye's pick: pay the chosen tier, and
        if it failed the hidden test, pay each higher tier until one passes --
        a wrong-cheap route costs the re-run, it doesn't save."""
        total, start = 0.0, TIERS.index(picks[task])
        for tier in TIERS[start:]:
            total += cost(task, tier)
            if passed(task, tier):
                return total, True
        return total, False  # nothing passed; charged the whole ladder

    out.append("")
    out.append("## Router arm -- what deadeye ACTUALLY picks (not the oracle)\n")
    out.append(f"`router.sh`, {len(rrows)//max(len(scored),1)} trial(s) per task, "
               f"judge fired on {judge_pct(judged, len(rrows))} of calls.\n")
    out.append("| Task | Oracle (cheapest that passed) | deadeye picked | Agree | Realized cost | Oracle cost |")
    out.append("|---|---|---|---|---|---|")
    r_tot = o_tot = b_tot = 0.0
    agree = 0
    for t in scored:
        o = cheapest_passing(t)
        d = picks[t]
        rc, ok = realized(t)
        oc = cost(t, o) if o else cost(t, "opus")
        r_tot += rc
        o_tot += oc
        b_tot += cost(t, "opus")
        hit = (o == d)
        agree += hit
        note = "" if ok else " (never passed)"
        out.append(f"| {t} | {o or 'none'} | {d} | {'yes' if hit else 'NO'} | "
                   f"${rc:.3f}{note} | ${oc:.3f} |")
    out.append("")
    out.append(f"- Agreement with the oracle: **{agree}/{len(scored)}**")
    out.append(f"- All-opus baseline: **${b_tot:.3f}**")
    out.append(f"- Oracle ceiling: **${o_tot:.3f}** ({pct_of(b_tot - o_tot, b_tot)} saved)")
    out.append(f"- **deadeye, realized: ${r_tot:.3f}** ({pct_of(b_tot - r_tot, b_tot)} saved)")
    if b_tot - o_tot > 0:
        out.append(f"- Share of the available ceiling captured: "
                   f"**{pct_of(b_tot - r_tot, b_tot - o_tot)}**")
    out.append("")
    if unstable:
        fail_open = len(rrows) - judged
        out.append(f"- **{len(unstable)}/{len(scored)} tasks did not route the same way on every "
                   f"trial** -- majority vote used for the table above:")
        for t, votes in unstable:
            out.append(f"  - `{t}`: {', '.join(votes)}")
        if fail_open == 0:
            out.append("  The judge answered on every call (0 fail-opens), so this is NOT the "
                       "heuristic fallback firing -- the classifier itself returns different "
                       "tiers for the same prompt. `claude -p` exposes no temperature/seed "
                       "control, so the router is stochastic by construction: the same subtask "
                       "can be routed differently between two runs, and the agreement figure "
                       "above is one sample from a distribution, not a fixed property.")
        else:
            out.append(f"  {fail_open}/{len(rrows)} call(s) fail-opened to the heuristic "
                       f"(judge errored or timed out), which accounts for some of this; the "
                       f"remainder is the classifier itself varying between runs.")
    else:
        out.append("- Every task picked the same tier on every trial in this run. That is one "
                   "sample, not a guarantee: `claude -p` exposes no temperature/seed control, "
                   "so stability has to be re-measured, never assumed.")
    out.append("- The realized number is what to quote for \"what does deadeye save\". "
               "The oracle is the ceiling it's trying to reach.")

open(os.path.join(HERE, "results/summary.md"), "w").write("\n".join(out) + "\n")
print("\n".join(out))
