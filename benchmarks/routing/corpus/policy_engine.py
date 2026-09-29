#!/usr/bin/env python3
"""Laya as a SIGNAL SOURCE, deadeye as the deterministic decider.

The architecture under test: Laya never names a model. It answers typed
sub-questions; deadeye's policy engine combines those with its own evidence,
hard safety rules and thresholds, and picks the model.

This is the one framing the earlier experiments never tested. They asked
"can Laya decide the tier?" (no). This asks "does Laya's evidence IMPROVE a
deterministic engine that already has its own?" -- a different question with a
different answer available.

Arms, all scored against the MAJORITY-CLASS null (not 1/3 -- these sets are
not balanced, and a constant answer beats 1/3 comfortably):

  A  deadeye kernel alone            -- the six builtin signals, judge off
  B  A + hard safety floor           -- a deterministic regex that may only RAISE
  C  A + Laya signals                -- leave-one-out fit, so nothing scores on its own fit
  D  A + floor + Laya                -- the full proposed engine
     Laya alone                      -- the control

Usage: ./policy_engine.py
"""
import collections, json, os, re
import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
MODEL_TIER = {"claude-haiku-4-5-20251001": 0, "claude-sonnet-5": 1, "claude-opus-5": 2}
LAYA_F = ["single", "spec", "existing", "security", "diagnose", "design"]

# Deterministic floor. May only raise a tier: being wrong costs money, never
# correctness. Measured separately at 100% tier-2 recall -- which turns out to
# be redundant, because the kernel already achieves that.
SECURITY = re.compile(
    r'\b(auth|authn|authz|oauth|jwt|token|credential|password|secret|crypt|tls|ssl|csrf|'
    r'xss|sql\s*inject|injection|privilege|escalation|vulnerab|exploit|pen[- ]?test|vapt|'
    r'security|sandbox|permission|acl|rbac)\w*\b', re.I)


def load_labels(name):
    raw = json.load(open(os.path.join(HERE, name)))
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


def main():
    text = {r["id"]: r["text"] for r in (json.loads(l) for l in open(os.path.join(HERE, "wild_sample.jsonl")))}
    laya = {r["id"]: r for r in (json.loads(l) for l in open(os.path.join(HERE, "wild_signals.jsonl")))}
    dx = {r["id"]: r for r in (json.loads(l) for l in open(os.path.join(HERE, "deadeye_signals.jsonl")))}

    for ver, (af, bf) in (("v1", ("labels_a.json", "labels_b.json")),
                          ("v2", ("labels_a2.json", "labels_b2.json"))):
        A, B = load_labels(af), load_labels(bf)
        ids = [i for i in sorted(set(A) & set(B)) if A[i] == B[i] and i in dx and dx[i].get("model")]
        y = np.array([A[i] for i in ids])
        counts = collections.Counter(y.tolist())
        null = max(counts.values()) / len(y) * 100
        print(f"\n=== {ver}: n={len(ids)}, dist {dict(sorted(counts.items()))}, "
              f"majority-class null {null:.1f}% ===")

        kernel = np.array([MODEL_TIER[dx[i]["model"]] for i in ids])
        floor = np.array([2 if SECURITY.search(text[i]) else 0 for i in ids])

        X = np.array([[1.0] + [laya[i][f] for f in LAYA_F] for i in ids])
        loo = np.zeros(len(ids), dtype=int)
        for k in range(len(ids)):
            m = np.ones(len(ids), bool); m[k] = False
            loo[k] = int((X[k:k + 1] @ fit(X[m], y[m])).argmax(1)[0])

        arms = [
            ("A  kernel alone", kernel),
            ("B  kernel + floor", np.maximum(kernel, floor)),
            ("C  kernel + laya", np.where(loo != kernel, np.maximum(kernel, loo), kernel)),
            ("D  kernel + floor + laya", np.maximum(np.maximum(kernel, floor), loo)),
            ("   laya alone (control)", loo),
        ]
        for name, pred in arms:
            acc = (pred == y).mean() * 100
            t2 = [pred[j] >= 2 for j in range(len(y)) if y[j] == 2]
            r2 = 100 * sum(t2) / len(t2) if t2 else float("nan")
            print(f"  {name:<26} {acc:5.1f}%  ({acc - null:+5.1f} vs null)   tier2 recall {r2:5.1f}%")

    print("\n  Robust across both label sets: Laya alone lands exactly on the null with")
    print("  0% tier-2 recall, and adding it never improves the kernel. The floor is")
    print("  redundant -- the kernel already reaches 100% tier-2 recall unaided, so the")
    print("  rule contributes only false positives. Whether the KERNEL looks good is")
    print("  label-set dependent (+17.1 on v2, -23.3 on v1); whether LAYA helps is not.")


if __name__ == "__main__":
    main()
