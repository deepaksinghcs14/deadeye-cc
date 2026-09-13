---
description: Dry-run the routing decision for a task, with reasoning
argument-hint: "[task description]"
allowed-tools: Bash(deadeye route:*), Bash(~/.deadeye/bin/deadeye route:*)
---

Run `deadeye route "$ARGUMENTS"` (or `deadeye route` with no arguments, to
dry-run against the current working tree's modified/staged files instead
of a task description). If that reports "command not found", it's very
likely just not on PATH -- deadeye never adds itself to PATH, it only
resolves its own binary internally for hook invocations. Retry with
`~/.deadeye/bin/deadeye route ...` before concluding it's missing.

Present the output as-is: the evidence each
signal provider contributed (or "none" if every provider had nothing to go
on), and the resulting Decision (model, effort, confidence, threshold,
reason). A threshold above the configured base means recorded escalations
for this task shape raised the bar -- the real Agent call uses the same
adjusted number.

This changes no state, but it is not free: with `mode.routing_judge=on`
(the default) an unsure decision spends one real `claude -p`
classification, the same one a live Agent call would. Say so if the user
is counting spend.

If evidence is empty, explain that this is expected and correct, not a
bug -- per INV-1, missing evidence must default to the most conservative
decision (the ceiling), never a cheaper one.

This command only reports what the kernel *would* decide -- it never
applies anything.
