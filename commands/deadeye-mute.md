---
description: Mute deadeye's advisories, plan-gate nags, and workflow hints for this session
argument-hint: "[off]"
---

The deadeye daemon handles this command itself through its prompt hook —
you don't need to run anything. Relay the confirmation line that appears
in your hook context (DEADEYE MUTED / DEADEYE UNMUTED) to the user.

If no confirmation appeared in context, the daemon likely isn't running;
tell the user any tool call will respawn it and they can try again.

Muting is session-scoped and silences nags only: advisories (repeat
reads/commands, large files, unbounded dumps, Grep limits), soft
plan-gate suggestions, and workflow hints. Silent Bash output rewrites
keep saving tokens, a `plan_gate: hard` setting keeps enforcing, and the
coder persona stays governed by `/deadeye-coder`. `/deadeye-mute off`
restores everything.
