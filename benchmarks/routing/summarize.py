#!/usr/bin/env python3
"""Aggregate results.jsonl into results/summary.md.

The honest claim has two halves: cost delta AND quality parity. A saving is only
real on a task where the cheaper tier actually PASSED its hidden test -- routing
to a tier that fails means a re-run on a stronger model, which is a cost, not a
saving. So "routed cost" uses, per task, the CHEAPEST tier that passed (the
oracle deadeye's router aims to approximate); baseline is everything on opus.
"""
import json, os, collections

HERE = os.path.dirname(__file__)
rows = [json.loads(l) for l in open(os.path.join(HERE, "results/results.jsonl")) if l.strip()]

TIERS = ["haiku", "sonnet", "opus"]
by = {(r["task"], r["tier"]): r for r in rows}
tasks = sorted({r["task"] for r in rows}, key=lambda t: (rows[[r["task"] for r in rows].index(t)]["tier_idx"], t))
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
    out.append(f"- {len(none)} task(s) failed on every tier in this SINGLE-TRIAL run: "
               f"{', '.join(none)}. A single trial is noisy near a model's capability "
               f"frontier -- do not read this as 'no tier can do it' (e.g. opus passed "
               f"h5-expr on a manual re-run). It is charged at opus in both arms, so it "
               f"claims no savings -- routing correctly keeps a frontier task on opus. "
               f"Multi-trial pass-rates are needed to place these precisely.")
out.append("- Savings above are the ORACLE ceiling (perfect tier choice). deadeye's "
           "router approximates this from cheap signals; a follow-up should measure "
           "router-vs-oracle agreement. Cache-heavy Claude Code system-prompt cost is "
           "included in every run and is similar across tiers, so it dilutes the "
           "headline %; the model-priced delta is the real lever.")

open(os.path.join(HERE, "results/summary.md"), "w").write("\n".join(out) + "\n")
print("\n".join(out))
