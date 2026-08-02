---
description: Show deadeye's current modes, kill switches, model catalog, and daemon health
allowed-tools: Bash(deadeye status)
---

Run `deadeye status` and present its output to the user, organized under these
headings: **Modes** (per-axis advise/enforce/off), **Kill switches** (flag any
that are OFF), **Catalog** (tier table, source, build date -- flag if stale),
**Daemon** (up/down), **Log** (path and record count). If the output includes
a `CLAUDE_EFFORT` note, surface it prominently -- it means the effort axis is
currently inert for this session, a real constraint, not decoration.

If the command is not found (`deadeye: command not found` or similar), tell
the user the binary hasn't been bootstrapped yet: it self-installs on first
hook invocation, so running any tool once in this session and then retrying
`/deadeye-status` should resolve it. Do not attempt to install it yourself.

Do not editorialize beyond what the output shows -- this command reports
state, it doesn't take action.
