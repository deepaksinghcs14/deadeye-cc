---
description: Dry-run deadeye's kernel decision for a task, with full reasoning
argument-hint: "[task description]"
allowed-tools: Bash(deadeye route:*)
---

Run `deadeye route "$ARGUMENTS"` (or `deadeye route` with no arguments, to
dry-run against the current working tree's modified/staged files instead
of a task description) and present the output as-is: the evidence each
signal provider contributed (or "none" if every provider had nothing to go
on), and the resulting Decision (model, effort, confidence, reason).

If evidence is empty, explain that this is expected and correct, not a
bug -- per INV-1, missing evidence must default to the most conservative
decision (the ceiling), never a cheaper one.

This command only reports what the kernel *would* decide -- it never
applies anything.
