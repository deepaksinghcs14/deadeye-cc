#!/usr/bin/env python3
"""Fit and cross-validate a DECOMPOSED tier classifier over Laya's sub-answers.

Laya answers one 3-way "which tier" question poorly: across this corpus it
predicts tier 1 for most inputs, so tier-1 recall looks perfect (22/22) while
tier-2 recall is 7/22 -- it misses the security-critical and subtle-debugging
work, which is the expensive direction to be wrong in.

Asking several ATOMIC yes/no questions in one forward pass and combining them
with FITTED weights does better. The fitting is the point: an earlier attempt
with hand-set 0.5 thresholds on the same sub-answers was worse than the
baseline on held-out cases. Laya's probabilities sit in a compressed band
(the server warns at startup that this checkpoint's temperatures are invalid
and its confidence uncalibrated), so arbitrary cutoffs do far too much work.

Usage:  ./fit.py            (reads corpus/signals.jsonl, written by extract)
"""
import json, os, random
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
FEATURES = ["single", "spec", "existing", "security", "diagnose", "design"]


def design(rows, feats):
    return np.array([[1.0] + [r[f] for f in feats] for r in rows])


def labels(rows):
    return np.array([r["tier"] for r in rows])


def fit(X, y, k=3, iters=4000, lr=0.5, l2=0.05):
    """Multinomial logistic regression by plain gradient descent.

    Deliberately dependency-light and deliberately regularized: 6 features
    plus a bias across 3 classes is 21 parameters, and the corpus is 66 rows.
    """
    W = np.zeros((X.shape[1], k))
    Y = np.eye(k)[y]
    for _ in range(iters):
        z = X @ W
        z -= z.max(1, keepdims=True)
        P = np.exp(z)
        P /= P.sum(1, keepdims=True)
        W -= lr * (X.T @ (P - Y) / len(y) + l2 * W)
    return W


def folds_of(rows, k, seed):
    rnd = random.Random(seed)
    by = {t: [r for r in rows if r["tier"] == t] for t in (0, 1, 2)}
    for v in by.values():
        rnd.shuffle(v)
    out = [[] for _ in range(k)]
    for t in (0, 1, 2):
        for i, r in enumerate(by[t]):
            out[i % k].append(r)
    return out


def main():
    rows = [json.loads(l) for l in open(os.path.join(HERE, "signals.jsonl")) if l.strip()]
    print(f"corpus: {len(rows)} cases, "
          + "/".join(str(sum(1 for r in rows if r['tier'] == t)) for t in (0, 1, 2)) + " per tier\n")

    print("signal separation (mean by true tier -- a spread near zero is noise):")
    for f in FEATURES:
        ms = [sum(r[f] for r in rows if r["tier"] == t) / sum(1 for r in rows if r["tier"] == t)
              for t in (0, 1, 2)]
        print(f"  {f:<10} " + " ".join(f"t{t}={m:.3f}" for t, m in enumerate(ms))
              + f"   spread {max(ms) - min(ms):.3f}")

    acc_d, acc_b = [], []
    for seed in range(5):
        fs = folds_of(rows, 6, seed)
        for i in range(6):
            te = fs[i]
            tr = [r for j, f in enumerate(fs) if j != i for r in f]
            W = fit(design(tr, FEATURES), labels(tr))
            acc_d.append(((design(te, FEATURES) @ W).argmax(1) == labels(te)).mean())
            acc_b.append((np.array([r["baseline"] for r in te]) == labels(te)).mean())
    d, b = np.array(acc_d), np.array(acc_b)
    print(f"\n{len(d)} folds (6-fold x 5 seeds):")
    print(f"  baseline single-question : {b.mean()*100:5.1f}%  (sd {b.std()*100:.1f})")
    print(f"  fitted decomposition     : {d.mean()*100:5.1f}%  (sd {d.std()*100:.1f})")
    print(f"  improvement              : {(d.mean()-b.mean())*100:+5.1f} points, "
          f"{(d>b).sum()} won / {(d==b).sum()} tied / {(d<b).sum()} lost")

    rec_d = {0: [], 1: [], 2: []}
    rec_b = {0: [], 1: [], 2: []}
    fs = folds_of(rows, 6, 99)
    for i in range(6):
        te = fs[i]
        tr = [r for j, f in enumerate(fs) if j != i for r in f]
        W = fit(design(tr, FEATURES), labels(tr))
        for r, p in zip(te, (design(te, FEATURES) @ W).argmax(1)):
            rec_d[r["tier"]].append(p == r["tier"])
            rec_b[r["tier"]].append(r["baseline"] == r["tier"])
    print("\n  per-tier recall, out-of-fold:")
    for t in (0, 1, 2):
        print(f"    tier {t}: baseline {sum(rec_b[t]):>2}/{len(rec_b[t])}   "
              f"decomposed {sum(rec_d[t]):>2}/{len(rec_d[t])}")
    print("\n  Tier 2 is the one that matters: missing it routes security-critical and")
    print("  subtle-debugging work to a cheap model. The baseline's perfect tier-1")
    print("  recall is an artifact of answering 1 for nearly everything.")

    W = fit(design(rows, FEATURES), labels(rows))
    print("\nweights fitted on the WHOLE corpus (for inspection, not for shipping --")
    print("see README on why these must be refitted on real traffic first):")
    for n, row in zip(["bias"] + FEATURES, W):
        print(f"  {n:<9} " + "  ".join(f"{v:+.3f}" for v in row))


if __name__ == "__main__":
    main()
