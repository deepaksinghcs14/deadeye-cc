# Tier definitions, v2

v1 left four questions unanswered, which two annotators then answered
differently 27 times out of 70. Each rule below exists to settle one of them,
and says WHICH TEST to apply rather than which answer to give.

**Tier is about the work the task demands, not about whether code changes.**
A task whose only output is a document can be tier 2; a task that edits twenty
files can be tier 0. Judge scope and specification, never how impressive the
subject sounds.

## 0 — self-contained and fully specified

One unit of work whose requirements are already complete: a single function,
file or package written from a full spec; a mechanical edit; **retrieving a
specific fact, file, symbol or location**; formatting; classification.
Fiddly-but-specified belongs here — parsing, precedence, concurrency
primitives are routine when the spec is complete.

Also tier 0: **executing a documented procedure** — following written steps,
running a defined checklist, verifying that something behaves as its own
documentation says.

## 1 — spans existing code, or the requirements must be inferred

Multi-file changes; editing unfamiliar code; integrating with an existing
system; an under-specified ask where part of the job is working out what is
wanted.

Also tier 1: **open-ended investigation** — searching across sources where
deciding *what counts as an answer* takes judgment (as opposed to retrieving a
specific known thing, which is tier 0).

Also tier 1: **reviewing or auditing code for defects not yet identified** —
scanning for bugs nobody has reported. The scope is "read this and look", and
it does not require diagnosing any particular failure.

## 2 — deep architecture, subtle debugging, or security-critical

Reserved for three things:

- **Diagnosing a specific reported symptom whose cause is unknown.** Something
  is observably wrong — an intermittent failure, a wrong result under load, a
  leak — and the work is finding out why. This is the distinction from tier 1's
  review: a review hunts for unknown bugs, this explains a known one.
- **Making an architecture or design decision** whose consequences are
  expensive to reverse — choosing a data model, a consistency guarantee, a
  migration strategy, a system boundary. Producing a *plan* that requires such
  a decision is tier 2 even though no code changes; gathering context to
  inform someone else's later decision is not.
- **Security-critical work** where being wrong is expensive: auditing auth,
  crypto, access control, or input handling for exploitable weakness, or
  designing any of them.

## Tie-breakers, in order

1. If torn between 0 and 1, choose **0**.
2. If torn between 1 and 2, ask: *is there a specific known failure to explain,
   an expensive-to-reverse decision to make, or an exploitable weakness to
   find?* If none of those, it is **1**.
3. Length, jargon and how senior the work sounds are not evidence. A long
   prompt describing a well-specified job is still tier 0.
