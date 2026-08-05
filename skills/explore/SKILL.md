---
name: explore
description: Progressive codebase exploration: index, then signatures, then targeted reads only.
context: fork
agent: Explore
---

Explore the codebase in three layers, stopping at the first one that
answers the question. Do not skip ahead to reading full files "to be safe"
-- each layer is a decision point, not a formality.

**Layer 1 -- file index.** `Glob`/`find` only, zero file reads. Get the
shape: directory layout, naming conventions, where the relevant area
likely lives. Stop here if the task is now obvious (e.g. "add a field to
this one struct" and you can see exactly which file that is).

**Layer 2 -- entry points and signatures.** `Grep` for the symbols,
exports, and call sites that matter to the task. You're building a map of
what calls what, not reading implementations yet. Stop here if you now
know which functions/files the change touches and roughly how they fit
together.

**Layer 3 -- targeted reads.** `Read` with `offset`/`limit` only for the
files directly relevant to the upcoming change -- never a full-repo read
sweep. If a file is large, read the section around the relevant symbol,
not the whole thing.

After each layer, explicitly decide: enough context to proceed, or one
more layer? Don't default to Layer 3 out of caution -- most tasks resolve
at Layer 1 or 2.

Return a structured summary to the caller: what you found, which
files/symbols matter, and your recommendation -- not the raw contents you
read along the way. The raw reads stay in this forked context; only the
summary crosses back.

**Before returning, cache the summary** so the next session doesn't
re-derive it (best-effort -- if this fails in any way, skip it silently
and return your summary as normal; it is a cache, never a deliverable):

```bash
deadeye notes-append explore <<'EOF'
<the question you were asked>
- <a file or symbol that matters, and why>
- recommendation: <one line>
EOF
```

Ten lines maximum. If `deadeye` isn't on PATH, retry once with
`~/.deadeye/bin/deadeye`; if that also fails, move on.
