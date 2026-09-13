---
description: Modes, coder level, kill switches, catalog, daemon health
allowed-tools: Bash(deadeye status), Bash(~/.deadeye/bin/deadeye status), Bash(deadeye doctor), Bash(~/.deadeye/bin/deadeye doctor)
---

Run `deadeye status`. If that reports "command not found", it's very likely
just not on PATH -- deadeye never adds itself to PATH, it only resolves its
own binary internally for hook invocations. Before concluding it's missing,
retry with the self-bootstrap install path directly:
`~/.deadeye/bin/deadeye status`. Only if that also fails is it genuinely not
bootstrapped yet.

Present the output to the user, organized under these headings: **Modes**
(per-axis advise/enforce/off), **Kill switches** (flag any that are OFF),
**Catalog** (tier table, source, build date -- flag if stale), **Daemon**
(up/down), **Log** (path and record count). If the output includes a
`CLAUDE_EFFORT` note, surface it prominently -- it means the effort axis is
currently inert for this session, a real constraint, not decoration.

If both invocations genuinely fail, tell the user the binary hasn't been
bootstrapped yet: it self-installs on first hook invocation, so running any
tool once in this session and then retrying `/deadeye-status` should resolve
it. Do not attempt to install it yourself.

Do not editorialize beyond what the output shows -- this command reports
state, it doesn't take action.

If the user's question is "is it WORKING" rather than "what is it set to" --
nothing seems to be happening, an advisory never appears, a setting seems
ignored -- run `deadeye doctor` instead and present that. `status` shows
configuration, which looks identical whether the plumbing under it is
healthy or broken; doctor checks the plumbing (binary resolution, state-dir
permissions, whether config.json parses, socket, daemon, whether the
routing judge can reach `claude`, hook-manifest coverage) and prints the fix
for each failure. Same PATH fallback applies.
