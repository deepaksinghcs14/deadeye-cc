#!/usr/bin/env python3
"""Evaluate baseline vs fitted decomposition on REAL Agent prompts.

Dataset 2. The point of it is that the TEXT is independent: 70 real Agent
tool prompts harvested from Claude Code transcripts across four projects,
written by Claude in past sessions rather than by anyone for this experiment.
Dataset 1 (corpus/tasks.jsonl) was written and labelled by one person, so a
model fitted on it could be learning that person's phrasing -- which
cross-validation cannot detect. This is the check for that.

Labels come from two independent annotators given only the tier definitions.
Their agreement is reported first and is the ceiling: if two annotators only
agree N% of the time, no classifier can meaningfully be scored above N% on
these labels, and the honest evaluation is restricted to the cases they agree
on.

Usage: ./eval_wild.py
"""
import json, os
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
F = ["single", "spec", "existing", "security", "diagnose", "design"]


def fit(X, y, k=3, iters=4000, lr=0.5, l2=0.05):
    W = np.zeros((X.shape[1], k)); Y = np.eye(k)[y]
    for _ in range(iters):
        z = X @ W; z -= z.max(1, keepdims=True)
        P = np.exp(z); P /= P.sum(1, keepdims=True)
        W -= lr * (X.T @ (P - Y) / len(y) + l2 * W)
    return W


def design(rows):
    return np.array([[1.0] + [r[f] for f in F] for r in rows])


def main():
    sig = {r["id"]: r for r in (json.loads(l) for l in open(os.path.join(HERE, "wild_signals.jsonl")))}
    A = {int(k): v for k, v in json.load(open(os.path.join(HERE, "labels_a.json"))).items()}
    braw = json.load(open(os.path.join(HERE, "labels_b.json")))
    B = {int(x["id"]): int(x["tier"]) for x in braw} if isinstance(braw, list) else \
        {int(k): int(v) for k, v in braw.items()}

    ids = sorted(set(A) & set(B) & set(sig))
    agree = [i for i in ids if A[i] == B[i]]
    print(f"labelled by both: {len(ids)}   agreed: {len(agree)} ({len(agree)/len(ids)*100:.0f}%)")
    print("  disagreements are excluded below: a label two annotators cannot agree on")
    print("  is not ground truth, and scoring against it measures noise.\n")
    cm = np.zeros((3, 3), int)
    for i in ids:
        cm[A[i]][B[i]] += 1
    print("  annotator agreement (rows=A, cols=B):")
    for t, row in enumerate(cm):
        print(f"    A={t}: " + " ".join(f"{v:>3}" for v in row))

    ev = [dict(sig[i], tier=A[i]) for i in agree]
    y = np.array([r["tier"] for r in ev])
    base = np.array([r["baseline"] for r in ev])
    print(f"\nevaluation set: {len(ev)} agreed cases, "
          + "/".join(str(int((y == t).sum())) for t in (0, 1, 2)) + " per tier")

    print(f"\n  baseline single-question : {(base == y).mean()*100:5.1f}%  "
          f"({int((base == y).sum())}/{len(ev)})")

    # Decomposition weights fitted on DATASET 1 only -- never on this data.
    d1 = [json.loads(l) for l in open(os.path.join(HERE, "signals.jsonl"))]
    W = fit(design(d1), np.array([r["tier"] for r in d1]))
    p = (design(ev) @ W).argmax(1)
    print(f"  decomposed (fit on ds1)  : {(p == y).mean()*100:5.1f}%  "
          f"({int((p == y).sum())}/{len(ev)})   <-- the transfer test")

    for name, pred in (("baseline", base), ("decomposed", p)):
        print(f"\n  {name} per-tier recall:")
        for t in (0, 1, 2):
            m = y == t
            if m.sum():
                print(f"    tier {t}: {int((pred[m] == t).sum())}/{int(m.sum())}")


if __name__ == "__main__":
    main()
