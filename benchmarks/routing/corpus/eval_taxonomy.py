#!/usr/bin/env python3
"""Did sharpening the tier definitions actually help? v1 vs v2, same 70 tasks.

The experiment this answers: at 61% inter-annotator agreement (v1), labels are
too noisy to fine-tune on or to score a classifier against. tiers_v2.md was
rewritten to settle the four ambiguities the v1 disagreements clustered into.
Two annotators then relabelled the SAME 70 real prompts from v2 alone.

Two things get measured, and they can come apart:

  1. Does agreement rise? That is the label-quality ceiling.
  2. Does classifier accuracy rise on the newly-agreed set? If agreement
     rises but accuracy stays at chance, the taxonomy was not the bottleneck
     and Laya simply cannot do this task -- which closes the question rather
     than leaving it open.

Usage: ./eval_taxonomy.py
"""
import json, os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
F = ["single", "spec", "existing", "security", "diagnose", "design"]


def load(name):
    p = os.path.join(HERE, name)
    raw = json.load(open(p))
    if isinstance(raw, list):
        return {int(x["id"]): int(x["tier"]) for x in raw}
    return {int(k): int(v) for k, v in raw.items()}


def fit(X, y, k=3, iters=4000, lr=0.5, l2=0.05):
    W = np.zeros((X.shape[1], k)); Y = np.eye(k)[y]
    for _ in range(iters):
        z = X @ W; z -= z.max(1, keepdims=True)
        P = np.exp(z); P /= P.sum(1, keepdims=True)
        W -= lr * (X.T @ (P - Y) / len(y) + l2 * W)
    return W


def design(rows):
    return np.array([[1.0] + [r[f] for f in F] for r in rows])


def report(tag, A, B, sig):
    ids = sorted(set(A) & set(B) & set(sig))
    agreed = [i for i in ids if A[i] == B[i]]
    pct = len(agreed) / len(ids) * 100
    print(f"\n=== {tag} ===")
    print(f"  annotator agreement: {len(agreed)}/{len(ids)} ({pct:.0f}%)")
    cm = np.zeros((3, 3), int)
    for i in ids:
        cm[A[i]][B[i]] += 1
    for t, row in enumerate(cm):
        print(f"    A={t}: " + " ".join(f"{v:>3}" for v in row))
    if not agreed:
        return pct, None, None
    ev = [dict(sig[i], tier=A[i]) for i in agreed]
    y = np.array([r["tier"] for r in ev])
    base = (np.array([r["baseline"] for r in ev]) == y).mean() * 100
    d1 = [json.loads(l) for l in open(os.path.join(HERE, "signals.jsonl"))]
    W = fit(design(d1), np.array([r["tier"] for r in d1]))
    dec = ((design(ev) @ W).argmax(1) == y).mean() * 100
    # The honest null is the MAJORITY CLASS, not 1/3. An unbalanced set makes
    # 33% look like a bar worth clearing when a constant answer already beats
    # it -- which is how a classifier that only ever says "1" was reported as
    # improving by +39.6 points.
    counts = [int((y == t).sum()) for t in (0, 1, 2)]
    null = max(counts) / len(y) * 100
    dist = "/".join(str(c) for c in counts)
    print(f"  agreed set: n={len(ev)} ({dist} per tier)")
    print(f"    baseline single-question : {base:5.1f}%   ({base-null:+.1f} vs null)")
    print(f"    decomposed (fit on ds1)  : {dec:5.1f}%   ({dec-null:+.1f} vs null)")
    print(f"    majority-class null      : {null:5.1f}%   <- the bar to clear")
    return pct, base - null, dec - null


def main():
    sig = {r["id"]: r for r in (json.loads(l) for l in open(os.path.join(HERE, "wild_signals.jsonl")))}
    v1 = report("v1 definitions", load("labels_a.json"), load("labels_b.json"), sig)
    v2 = report("v2 definitions (sharpened)", load("labels_a2.json"), load("labels_b2.json"), sig)

    print("\n=== verdict ===")
    print(f"  agreement : {v1[0]:.0f}% -> {v2[0]:.0f}%   ({v2[0]-v1[0]:+.0f} points)")
    if v1[1] is not None and v2[1] is not None:
        print(f"  baseline  : {v1[1]:+.1f} -> {v2[1]:+.1f} vs null")
        print(f"  decomposed: {v1[2]:+.1f} -> {v2[2]:+.1f} vs null")
        if v2[1] <= 0 and v2[2] <= 0:
            print("\n  Both classifiers are at or below the majority-class null on both")
            print("  label sets. They carry no information about this distribution: a")
            print("  constant answer does as well or better. The taxonomy is not the")
            print("  binding constraint, and fine-tuning would be training on a task the")
            print("  model shows no signal on.")
        else:
            print("\n  A classifier clears the majority-class null. That is a real signal")
            print("  and worth building on.")


if __name__ == "__main__":
    main()
