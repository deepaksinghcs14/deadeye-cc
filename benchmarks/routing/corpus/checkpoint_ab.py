#!/usr/bin/env python3
"""Does domain fine-tuning transfer? A natural experiment, free to run.

Convai already did the experiment we are contemplating: `typed-decisions` is
the `english` checkpoint fine-tuned on ~2,000 decisions across four workflows
(invoice processing, customer service, security incidents, agent traces).
They report 0.362 -> 0.766 on that domain, +40.4 points.

Running both checkpoints over OUR data answers, for free, the question that
would otherwise cost days of work: does a domain fine-tune help outside its
domain? If their +40 shows up on our tasks, fine-tuning is general. If it
vanishes or inverts, fine-tuning specialises hard -- which is an argument FOR
fine-tuning on our own domain, not against it.

Requires laya-serve with both checkpoints reachable (LAYA_MAX_LOADED>=2, or
accept a cold load on the first call to each).

Usage: ./checkpoint_ab.py
"""
import json, os, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
EP = os.environ.get("LAYA_EP", "http://127.0.0.1:8791") + "/v1/systemone"
INSTR = ("Judge by SCOPE and SPECIFICATION, not by how advanced the topic sounds. A "
         "self-contained, fully-specified piece of work is tier 0 even when the algorithm is "
         "fiddly. Classify how much capability this software subtask needs. If torn between 0 "
         "and 1, choose 0.")
CRIT = {
    "0": "one self-contained, clearly specified unit of work: a single function, file, or package written from a complete spec; a mechanical edit; search; lookup; formatting; classification. Fiddly-but-specified belongs here.",
    "1": "work that spans or modifies existing code, or whose requirements must be inferred: multi-file changes, editing unfamiliar code, integrating with an existing system, an under-specified ask.",
    "2": "deep architecture decisions, subtle or tricky debugging, or security-critical work where a wrong answer is expensive.",
}


def tier(text, model):
    body = json.dumps({"state": {"task": text}, "model": model,
                       "questions": {"q": {"type": "choice", "instructions": INSTR,
                                           "criteria": CRIT}}}).encode()
    req = urllib.request.Request(EP, data=body, headers={"content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as f:
        return int(json.load(f)["answers"]["q"]["choice"])


def main():
    A = {int(k): v for k, v in json.load(open(os.path.join(HERE, "labels_a.json"))).items()}
    B = {int(x["id"]): int(x["tier"]) for x in json.load(open(os.path.join(HERE, "labels_b.json")))}
    wild = {r["id"]: r for r in (json.loads(l) for l in open(os.path.join(HERE, "wild_sample.jsonl")))}
    agreed = [i for i in sorted(set(A) & set(B)) if A[i] == B[i]]
    syn = [json.loads(l) for l in open(os.path.join(HERE, "signals.jsonl"))]

    for name, items, label in (("REAL prompts (annotator-agreed)", [(wild[i]["text"], A[i]) for i in agreed], None),
                               ("SYNTHETIC corpus", [(r["text"], r["tier"]) for r in syn], None)):
        print(f"\n=== {name}, n={len(items)} ===")
        got = {}
        for model in ("english", "typed-decisions"):
            ok = sum(1 for t, y in items if tier(t, model) == y)
            got[model] = ok
            print(f"  {model:<16} {ok:>3}/{len(items)}  = {ok/len(items)*100:5.1f}%")
        d = (got["typed-decisions"] - got["english"]) / len(items) * 100
        print(f"  fine-tune delta  {d:+.1f} points")

    print("\n  Their reported delta on THEIR domain: 0.362 -> 0.766 = +40.4 points.")
    print("  A large in-domain gain that does not transfer is evidence that fine-tuning")
    print("  SPECIALISES -- an argument for fine-tuning on our own domain, gated on")
    print("  label quality (see eval_wild.py: annotators agree only 61% of the time).")


if __name__ == "__main__":
    main()
