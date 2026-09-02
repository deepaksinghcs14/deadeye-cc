# Verified findings — Phase 0

Claude Code **v2.1.220**, darwin/arm64, go1.26.5, tested 2026-08-02.

Two kinds of evidence appear below: **live** (a real `claude -p --plugin-dir`
headless session running this repo's actual plugin, or a real installed
plugin's shipped files) and **binary-introspected** (a research agent read
the Claude Code binary's own zod schemas and error strings, without running
a session). Live evidence is marked accordingly; everything else is
binary-introspected. Docs alone were not trusted as a source — see PLAN.md's
own instruction to verify empirically.

Methodology for the live findings: built `deadeye`, put it on `PATH`, ran
`claude -p "<task>" --plugin-dir <this-repo> --debug-file <path> -d hooks`
against this repo's actual `hooks/hooks.json` and daemon, and read Claude
Code's own hook-output validator errors (which echo its accepted schema
verbatim) plus its plugin-load debug trace.

---

## §10.3 — PreToolUse / hook JSON contract

**Live**, from real captured payloads now in `testdata/payloads/` (see
`internal/hookio/types_test.go`'s `TestGoldenPayloads`):

**Input** (`hook_event_name`-keyed, common fields across events):
`session_id`, `transcript_path`, `cwd`, `prompt_id`, `permission_mode`,
`effort:{level}`, `hook_event_name`. Event-specific: `PreToolUse`/
`PostToolUse` add `tool_name`, `tool_input`, `tool_use_id`; `PostToolUse`
additionally carries `tool_response` (tool-specific shape, e.g.
`{stdout,stderr,interrupted,isImage,noOutputExpected}` for Bash) and
`duration_ms` -- neither is in `hookio.Input` yet since nothing consumes
them in Phase 0; add them when Phase 1's `/deadeye-audit` needs real
bytes-saved measurement instead of an estimate. `SessionStart` adds
`source` (e.g. `"startup"`), no `effort`/`prompt_id`. `SubagentStart` adds
`agent_id`, `agent_type`, no `effort`. `Stop` adds `stop_hook_active`,
`last_assistant_message`, `background_tasks`, `session_crons`. `SessionEnd`
adds `reason`.

**Output — CRITICAL correction, two real bugs caught live:**

1. **`hookSpecificOutput.hookEventName` is REQUIRED, not optional.** The
   first draft of `internal/hookio` (written from docs alone) marked it
   `omitempty`. Live test: the daemon returned
   `{"hookSpecificOutput":{"additionalContext":"..."}}` (no `hookEventName`)
   for a real `UserPromptSubmit` hook call; Claude Code's own validator
   rejected the entire response:
   `Hook JSON output validation failed — hookSpecificOutput is missing
   required field "hookEventName"`. Fixed: `hookio.HookSpecificOutput.
   HookEventName` has no `omitempty`, and `hookio.ForEvent(event)` is now
   the only sanctioned constructor so this can't regress (see
   `TestForEventSetsRequiredHookEventName`).

2. **`SessionStart` does not accept `hookSpecificOutput` at all.** The
   validator's own schema dump (triggered by the bug above, and captured
   verbatim in the debug log) lists exactly five events under
   `hookSpecificOutput`: `PreToolUse` (`permissionDecision` incl. `"defer"`,
   `permissionDecisionReason`, `updatedInput`), `UserPromptSubmit`
   (`additionalContext`, marked **required** for that event), `PostToolUse`
   (`additionalContext`, optional), `PostToolBatch` (`additionalContext`,
   optional), `Stop`/`SubagentStop` (`additionalContext`, optional).
   **`SessionStart` is absent from that union.** See §5.1 finding below for
   what this means for PLAN.md.

Full confirmed top-level `Output` schema (from the validator's error dump,
live): `continue` (bool), `suppressOutput` (bool), `stopReason` (string),
`decision` (`"approve"|"block"`), `reason` (string), `systemMessage`
(string), `terminalSequence` (string), `permissionDecision`
(`"allow"|"deny"|"ask"` -- no `"defer"` at top level, unlike PreToolUse's
nested one), `hookSpecificOutput` (per-event, above).
`internal/hookio.Output` now mirrors this exactly.

---

## §5.1 finding — SessionStart cannot inject model-visible context (kills PLAN.md §5.1 as written)

> **SUPERSEDED by §11 (2026-08-03):** raw stdout DOES inject at
> SessionStart — only the JSON fields tested below are dead. Read §11
> before relying on anything in this section.

**Live, definitively confirmed**, three-part test:

1. Daemon returned `{"hookSpecificOutput":{"additionalContext":"PROBE"}}`
   for `SessionStart` → **rejected outright** by the validator (see above;
   not a supported field for this event).
2. Daemon returned `{"systemMessage":"DEADEYE_V6_PROBE_..."}` for
   `SessionStart` → **accepted, no validation error**, hook logged
   `success` -- but the probe string never reached the model. Prompted the
   model directly to repeat back anything starting with
   `DEADEYE_V6_PROBE`; it reported none.
3. **Positive control**, same session: daemon returned
   `hookSpecificOutput.additionalContext` for `UserPromptSubmit` (with
   `hookEventName` correctly set) → the model *did* report seeing it,
   verbatim, attributed to "a system-reminder tagged as 'UserPromptSubmit
   hook additional context'". This confirms the test methodology is valid
   -- the negative results above are real, not a broken probe.

**Conclusion:** in Claude Code v2.1.220, `SessionStart` hook output cannot
put anything into the model's context. `systemMessage` is accepted by the
schema but appears to be a human/CLI-facing display field (plausibly a
toast/status line in interactive mode), not part of the prompt -- and
`-p`/print mode has no visible surface for it at all. **PLAN.md §5.1's
entire mechanism -- injecting the tier table, effort guidance, and
workflow-script guidance once at `SessionStart` -- does not work and must
be redesigned before Phase 2.** The likely fix: inject via
`UserPromptSubmit`'s `additionalContext` instead (confirmed working), gated
to fire only once per session (e.g. a `first_prompt_seen(session_id)` flag
in daemon state) so the INV-4 byte-stability requirement still holds --
same content, one time, not appended to every turn. This is now the
concrete Phase 2 blocker in place of the "confirm this works" note the
plan carried; needs a design decision before Phase 2 starts, not just
confirmation.

---

## §10.1 — Does PreToolUse fire inside subagents / the Agent tool call itself?

**Live, confirmed.** Real session: `claude -p` with a prompt that delegates
to a `general-purpose` subagent via the Agent tool. Captured, in order:
`PreToolUse` with `tool_name:"Agent"` (the delegation call itself) →
`SubagentStart` (with `agent_id`, `agent_type:"general-purpose"`) →
`PostToolUse` with `tool_name:"Agent"`. So **PreToolUse does fire for the
Agent tool call**, with the full `tool_input` visible (see §10.2). Whether
it additionally fires for tool calls made *inside* the subagent's own
turns (as opposed to the delegation call) was not exercised by this test
(the subagent only produced a text answer, no tool calls of its own) --
still open for Phase 3.

## §10.2 — Does the Agent tool's input schema accept `effort`?

**Live, confirmed no.** Real captured `tool_input` for a `PreToolUse`/
`Agent` call: `{"description":"...", "prompt":"...",
"subagent_type":"general-purpose", "run_in_background":false}` -- no
`model`, no `effort` key present in this invocation (the caller didn't set
one). Corroborated structurally: the Agent tool's own schema (visible to
this session as a first-class tool definition) lists
`description, isolation, model, prompt, run_in_background, subagent_type`
-- **no `effort` parameter exists on the tool at all.** Effort enforcement
for subagents (Phase 3) is frontmatter-only (`effort:` in the agent's own
`.md` file, or the daemon rewriting `tool_input.model` -- never
`tool_input.effort`, because there's nowhere to put it).

## §10.4 — Can a hook force permission mode?

**Binary-introspected**, not re-verified live this session (unchanged from
the prior finding): `permission_mode` is read-only on `PreToolUse`.
`PermissionRequest` hooks can set session mode via
`decision.updatedPermissions:[{type:"setMode",...}]`, but that's a
different, narrower hook event than the ones exercised live here. Relevant
to Phase 4's plan gate; not re-tested.

---

## V1/V2 — Plugin manifest and marketplace mechanics

**Both live and binary-introspected**, and they agree:

- **`hooks/hooks.json` at the plugin root is auto-loaded.** Declaring
  `"hooks": "./hooks/hooks.json"` in `plugin.json` for that exact path is
  not just redundant, it's a **hard error**: live test with that field
  present produced `[ERROR] Duplicate hooks file detected: ./hooks/hooks.json
  resolves to already-loaded file ... The standard hooks/hooks.json is
  loaded automatically, so manifest.hooks should only reference additional
  hook files.` Fixed by removing the `hooks` key from `plugin.json`
  entirely (confirmed clean: 0 occurrences of "Duplicate hooks file" or
  "validation failed" in the debug log after the fix). The `hooks` field
  in `plugin.json` exists only for a *nonstandard* hooks filename (e.g.
  ponytail's `hooks/claude-codex-hooks.json`, shared with other agent
  hosts) or *additional* hook files beyond the standard one.
- Binary-introspected, from the plugin manifest's zod schema: `commands`,
  `agents`, `skills`, `outputStyles` behave the same way as `hooks` --
  declaring the field for the *standard* directory name is either
  redundant or (for `commands`/`agents`/`outputStyles`, per the schema
  description) actually **replaces** auto-loading rather than erroring;
  `skills` is the one exception, additive. Since this plugin doesn't
  declare any of `commands`/`agents`/`skills` in `plugin.json`, all three
  standard directories auto-load normally -- confirmed for `commands/` and
  `hooks/` live (the `/deadeye-status` command and all seven registered
  hook events loaded without complaint once the duplicate-hooks bug was
  fixed).
- **Marketplace `name` is explicit and authoritative, not derived from the
  repo.** Binary-introspected: `marketplace.json`'s `name` field is
  required (not optional) in the manifest schema, and the loader resolves
  by that name; corroborated by install/overwrite log strings
  (`` Marketplace '${o.name}' exists with different source — overwriting ``)
  and by the observed installed-plugin registry (`ponytail@ponytail`, keyed
  off `marketplace.json.name`, not the repo path). Confirms the naming
  section's correction: install line is `/plugin install deadeye@deadeye`.
- `claude plugin validate <path> --strict` passes clean on this repo's
  `.claude-plugin/` (validated separately as marketplace manifest and as
  plugin manifest).

## V9 — No `rules/` component in the plugin system at all

**Binary-introspected**, resolves §10.9 definitively (not merely
"unconfirmed" as the prior draft had it): the plugin manifest schema has
**no `rules` field**, and no installed plugin (ponytail, the only one
present) ships a Claude Code `rules/` directory -- its `.cursor/rules/`,
`.windsurf/rules/`, etc. are for other agent hosts entirely, unrelated to
Claude Code's own rules mechanism. Claude Code's native rules loader only
reads `.claude/rules/**.md` at the **project, user, or managed-settings**
level (never from a plugin), keyed on a `globs:` frontmatter field -- not
`paths:`. **`paths:` is a *skill* frontmatter field**
(`"Glob patterns this skill applies to. The skill only loads when the
model touches matching files."`), not a rules field. Conclusion for
PLAN.md §5.6: a plugin cannot ship path-scoped rules at all. The only
path-scoped mechanism available *inside* a plugin is a skill with a
`paths:` frontmatter constraint -- reframe the "anti-waste rules" delivery
around that, not a `rules/` directory, when Phase 1 gets there.

---

## V7 — Catalog: no reachable pricing API from the hook environment

**Live**, unchanged from the pre-build check: the hook environment carries
no `ANTHROPIC_API_KEY` (OAuth-based); `GET https://api.anthropic.com/v1/models`
returns `401`. Binary-introspected addition: even authenticated, `/v1/models`
carries no pricing field, only `capabilities.effort:{low,medium,high,
xhigh,max}` per model. Real pricing (fetched live from
https://platform.claude.com/docs/en/about-claude/pricing, 2026-08-02) is
seeded by hand into `scripts/gen-catalog.go`, which regenerates
`internal/catalog/catalog_gen.go`:

| Model | Input $/MTok | Output $/MTok | Tier |
|---|---|---|---|
| claude-haiku-4-5-20251001 | 1 | 5 | 0 |
| claude-sonnet-5 (through 2026-08-31) | 2 | 10 | 1 |
| claude-opus-5 | 5 | 25 | 2 |
| claude-fable-5 | 10 | 50 | 3 |

Ranked by blended price (`input + 4*output`); tier order matches family
order here, so no price/family disagreement warning fires.

## V8 — Daemon latency

**Live**, in-process benchmark (`cmd/deadeye/daemon_test.go`,
`TestDaemonRoundTripP95`; same code path a real hook exercises, minus
process-spawn overhead, which only tightens the bound): 30 round trips
through a live daemon, p95 ≈ 400-600µs, comfortably under INV-8's 50ms
budget. Comparison baseline (pre-build check): a bare Go binary printing
`{}` with no daemon at all measured p95 16.5ms over 30 cold starts on the
same machine -- also well under budget. The daemon was built per the
approved Phase 0 scope decision, not because latency required it.

**Known gotcha for anyone extending this test:** unix domain socket paths
are capped at ~104 bytes on macOS/BSD. `t.TempDir()` nests under a long
per-test `$TMPDIR` path that blows past that limit silently -- `net.Listen`
just fails and `runDaemon()` returns early with no visible error (by
design, fail-open). Use a short directory directly under `/tmp` for any
test that stands up a real daemon socket.

---

## Live end-to-end confirmation (Definition of Done items 6/7/9)

Ran `claude -p <task exercising Write, Edit, Bash, and an Agent-tool
subagent delegation> --plugin-dir <this repo> --debug-file <path> -d hooks`
against the fixed `plugin.json`/`hookio` twice (once to find the bugs above,
once clean after fixing them). Final clean run: **0** validation-failed
lines, **0** duplicate-hooks-file lines, all 7 registered events captured
(`SessionStart`, `UserPromptSubmit`, `PreToolUse`×4, `PostToolUse`×4,
`SubagentStart`, `Stop`, `SessionEnd`), all logged as `noop` decisions in
`~/.deadeye/decisions.jsonl`, daemon self-bootstrapped from cold on the
first hook call and stayed up for the rest of the session. The `deadeye
status` CLI (what `/deadeye-status` shells out to) renders correctly with
the daemon both up and down (`deadeye uninstall` between runs, confirmed
`Daemon: down`).

**Minor open item:** the plugin loader debug log confirms
`Loaded 1 commands from plugin deadeye default directory` -- the command
file itself is discovered and well-formed. But invoking `/deadeye-status`
as the literal prompt text of a `claude -p` call returned
`Unknown command: /deadeye-status` rather than running it. Not chased
further this session; most likely `-p`/print mode doesn't dispatch slash
commands from literal prompt text the way an interactive session does
(headless automation isn't really `/deadeye-status`'s use case), rather
than a defect in `commands/deadeye-status.md`. Confirm in an interactive
session before relying on this.

Not done this session: installing via the real `/plugin marketplace add` +
`/plugin install` flow inside an *interactive* session (item 6's literal
form) -- `claude plugin validate --strict` plus the `--plugin-dir` headless
runs above are the closest equivalent achievable non-interactively, and the
marketplace-name mechanics were independently confirmed via binary
introspection (V1/V2). Recommend the user run the real
`/plugin marketplace add` / `/plugin install` once interactively before the
first public release, to catch anything specific to that path.

---

## V10 (Phase 3) -- CORRECTION to §10.3/V3: UpdatedInput does NOT merge, it replaces

**Live, and load-bearing -- this overturns part of an earlier "confirmed"
finding.** §10.3/V3 above states "`updatedInput` has MERGE semantics on the
Claude Code side: only the keys present here override tool_input, everything
else passes through unchanged." That was wrong, or at best incomplete, and
the Phase 0 test that produced it couldn't have caught the error: it only
ever exercised Bash preprocessing, and Bash's `tool_input` schema has no
required fields besides the one being rewritten (`command`), so a bare
`{"command": "..."}` replacement is indistinguishable from a merge in that
one case.

Phase 3 wired routing enforcement to rewrite `tool_input.model` for the
Agent tool via `updatedInput: {"model": "haiku"}` alone. Live result:

```
[WARN] PreToolUse hook for Agent returned updatedInput that failed schema
validation: Agent failed due to the following issues:
The required parameter `description` is missing
[DEBUG] Hook denied tool use for Agent
```

Claude Code validates `updatedInput` as if it WERE the complete new
`tool_input`, not a patch merged onto the original. The model itself
noticed and reported the Agent tool "failing every retry" due to the
hook stripping its own required fields -- a real, user-visible failure
mode, not just a log line.

**Fix:** `internal/hookio.MergeToolInput` (used by both the Bash-preprocess
and Agent-routing rewrite paths now) reads the original `tool_input`,
overlays only the intended change, and re-marshals the full object as
`updatedInput`. Re-tested live after the fix: `modified tool input keys:
[description, model, prompt, run_in_background, subagent_type]` -- all
five original keys present, zero validation failures, the Agent call
succeeded.

**Lesson for future phases:** never send a partial `updatedInput` for any
tool with more than one required field, regardless of what §10.3's or any
other docs-derived note says about merge semantics -- always merge
client-side. §10.3 above is left as originally written (with this note)
rather than silently edited, since the mistake and how it was caught are
worth keeping visible.

## Live confirmation (Phase 3) -- subagent model routing

Same headless-session methodology, `mode.routing` toggled via
`~/.deadeye/config.json`. **Advise mode** (default): a real Agent-tool
delegation received `additionalContext` reading `deadeye recommends
model=... effort=... -- <reason>`, call proceeded unmodified. **Enforce
mode**: after the merge fix above, the delegation's `tool_input.model` was
rewritten to the family alias (`"fable"`, matching the kernel's ceiling
decision for a file-scope-less trivial task -- no evidence available to
justify a downshift, exactly the INV-1-conservative outcome expected), the
call succeeded, zero schema-validation failures across three consecutive
enforced delegations in one session.

## Live confirmation (Phase 4) -- plan gate

**Soft layer:** a real vague/implementation-shaped prompt ("not sure,
maybe look into a redesign... across the codebase") got the plan-first
suggestion appended to the same UserPromptSubmit additionalContext as the
session injection -- confirmed both land together in one response when a
turn trips both.

**Hard layer:** with `mode.plan_gate: "hard"` set, a prompt that tripped
the soft trigger and then attempted `Write` got `permissionDecision: "ask"`
on `PreToolUse/Write`; Claude Code's own log: `Write tool permission
denied`, and the file was confirmed never created. **New finding: in
headless `-p` mode (no TTY), an `ask` decision auto-denies rather than
blocking for input** -- there's no interactive prompt to answer, so
Claude Code resolves it to the safe (deny) side automatically. In an
interactive session this would surface the native permission prompt
instead (not re-verified interactively this session, but the mechanism
--`permissionDecision:"ask"` on PreToolUse -- is the same one used
successfully elsewhere, e.g. every real permission prompt a user sees
day to day).

## V11 (Phase 6) -- downshifting was architecturally unreachable; two real bugs, found live

**Live, and the most consequential bug caught this session** -- not a
Claude Code quirk this time, but a real defect in this plugin's own
kernel calibration that silently made Phase 2/3's downshift path dead
code for essentially all real usage.

**Bug 1.** `internal/signals`'s builtin providers were calibrated with
confidence values (promptshape 0.35, filescope 0.6, gitchurn 0.5,
testpresence 0.55) all below the config default `downshift_threshold`
(0.8). `kernel.Decide` requires the MINIMUM confidence across ALL
evidence to clear that threshold before downshifting at all (by design,
for a clean INV-1 guarantee -- see kernel_test.go's property-test doc
comment). Since every builtin's confidence sat below 0.8, downshifting
was **unreachable through any combination of the four builtin providers**
-- `/deadeye-route` and every real Agent delegation always landed on the
ceiling, and this went unnoticed through Phases 2-5 because nothing
ever exercised the downshift path with live evidence until Phase 6's
escalation detection needed a genuine downshift to escalate FROM.

Root cause: these are deterministic, cheap measurements (a file count, a
git log count, a file-existence check), not fuzzy LLM guesses -- they
deserved confidence in the 0.8+ range, not the arbitrary 0.5-0.6 values
they'd been given. Fixed: filescope 0.85, gitchurn 0.82, testpresence 0.8.
Added `TestDownshiftIsReachableThroughRealProviders`
(`internal/kernel/integration_test.go`), an integration test running the
actual builtin providers against a real git repo, asserting the result is
NOT the ceiling -- this is what caught the *second* bug below when the
first fix wasn't quite sufficient (gitchurn at 0.75 still fell short).

**Bug 2, found live after Bug 1's first fix.** `promptshape`'s confidence
was keyed to `score > 0` (any nonzero complexity contribution drops it to
0.35). But the score includes a `+0.1` per literal `?` in the prompt --
and real Agent delegations very commonly end in a question mark ("what is
2+2?") with zero genuine complexity or vagueness. That alone was enough
to drop confidence to 0.35 and re-block every downshift, invisible to the
unit tests (which used prompts without trailing "?") but immediately
visible trying to reproduce escalation detection live: a plain "what is
2+2?" delegation landed on the ceiling instead of downshifting. Fixed by
gating confidence on whether an actual complexity/vague KEYWORD matched
(the genuinely fuzzy keyword-to-magnitude translation), not on the
combined score, which also includes objective, exactly-countable
contributions (question-mark count, word count) that aren't guesses at
all.

**Live confirmation, post-fix:** `deadeye route "rename the variable x to
count in a.go"` now genuinely downshifts (tier 0, low effort, confidence
0.85) instead of always showing the ceiling. A two-delegation session (no
model → then explicit `model: opus`) correctly downshifted the first
call, detected the second as an escalation against the first's task
shape, recorded it to `outcomes.jsonl`, and `/deadeye-audit` reported it
-- confirmed end to end.

## 11. SessionStart raw stdout DOES inject (supersedes §5.1's conclusion)

§5.1 concluded SessionStart "cannot put anything in the model's context"
-- but every probe behind that conclusion used JSON fields
(`hookSpecificOutput`, `systemMessage`). Re-verified 2026-08-03 on the
then-current Claude Code with the missing case: **raw plain text written
to stdout**.

Method: a scratch project with a `.claude/settings.json` SessionStart
hook running `echo "DEADEYE_PROBE_RAW_7f3a91 ..."`, then
`claude -p "report any DEADEYE_PROBE tokens in your context"`. The model
replied with the exact nonce. Conclusion: **SessionStart raw stdout
reaches model context**; it was only ever the JSON-field forms that were
dead. (Independent corroboration: the ponytail plugin injects its whole
ruleset this way in production, including on `source: compact` events --
its banner observably lands in post-compaction context.)

Same run, two more results:

- **SubagentStart `hookSpecificOutput.additionalContext` reaches the
  subagent** (previously UNVERIFIED here): a SubagentStart hook emitting
  a nonce via that JSON shape, parent asked to spawn a subagent that
  reports probe tokens -- the subagent returned the nonce verbatim. This
  also retroactively validates 0.4.0's subagent brevity note delivery.

- **Slash commands reach UserPromptSubmit as the literal typed string --
  but only for commands that exist.** `/ponytail spotter` (installed
  plugin) arrived as `"prompt": "/ponytail spotter"` (payload:
  `testdata/payloads/userpromptsubmit_slash_command.jsonl`). An unknown
  `/deadeye-coder sniper` produced NO UserPromptSubmit event at all --
  the CLI rejects unknown slash commands before the hook fires. So any
  prompt-parsing mode tracker only sees its command once the
  corresponding skill/command file is shipped.

## 12. Codex CLI hooks — live verification (codex-cli 0.142.5, 2026-08-04)

Probed with real `codex exec` runs on this machine (hooks feature flag
`[features] hooks = true` in config.toml; `--dangerously-bypass-hook-trust`
for automation; interactive use goes through Codex's hook trust review).

- **SessionStart `hookSpecificOutput.additionalContext` REACHES the model**
  (nonce probe answered verbatim). Unlike Claude Code (§11), no raw-stdout
  workaround needed — Codex takes the JSON path on this surface.
- **PreToolUse `updatedInput` rewrites the executed command**: a hook
  returning `{"updatedInput":{"command":"echo rewritten-by-deadeye"}}` for
  `echo original` produced stdout `rewritten-by-deadeye`. Context hygiene
  ports whole.
- Payload field names are identical to Claude Code's (`session_id`, `cwd`,
  `hook_event_name`, `tool_name`, `tool_input.command`, `tool_use_id`,
  `permission_mode`) plus `turn_id` and `model`. SessionStart carries the
  same `source: "startup"` convention. `hookio.Input` parses Codex
  payloads with zero changes. Captured in `testdata/payloads/codex/`.
- Shell tool is `"Bash"`; edits are `"apply_patch"` with the patch text in
  `tool_input.command`. PreToolUse fires before the sandbox decision.
- `tool_response` on PostToolUse is a plain JSON string (Claude sends
  structured JSON) — `json.RawMessage` handles both.
- `SessionEnd` never fired across several `codex exec` runs — do not rely
  on it (session-memory writes get no trigger on codex exec; interactive
  behavior unverified).
- UNVERIFIED (not probed): PostCompact injection (needs a compaction,
  impractical to trigger non-interactively — registered anyway, harmless
  if silent), `mcp__` tool-name prefix, hooks on Windows (docs say
  unsupported).
- `codex exec` gotchas for E2E scripts: pass `--skip-git-repo-check` or
  run in a trusted dir, and close stdin (`< /dev/null`) or it waits.

## 13. Gemini CLI — designed against docs, live verification pending (experimental)

Gemini CLI is engine-tier by capability (its hooks run an external command
with JSON stdin/stdout, `BeforeTool` can rewrite `hookSpecificOutput.tool_input`,
`SessionStart`/`BeforeAgent` inject `additionalContext`), with one gap: its
permission model is deny-only — no `ask`/`allow`. Sources:
gemini-cli `docs/hooks/reference.md`. deadeye's `--host gemini` path is
implemented against those docs; the notes below distinguish what is
verified from what awaits a live install.

**Verified by shape (this environment, synthesized payloads through the
real daemon):**
- `SessionStart --host gemini` returns the coder persona via
  `hookSpecificOutput.additionalContext` (not raw stdout — the reduced-host
  path), no `permissionDecision` leak.
- `UserPromptSubmit --host gemini` returns session guidance, codemap, and
  the dep-flag via `additionalContext`, correctly reduced (no Agent-tool
  tier table, effort, or workflow line — Gemini has no subagent surface).
- The output translator (`hookio.MarshalGemini`) renders `updatedInput` →
  `tool_input`, `permissionDecision:"ask"`+AskFallback deny → top-level
  `decision:"deny"`+reason, ask+advise → `additionalContext`. Unit-tested
  both branches.

**Registered this release:** only `SessionStart` and `BeforeAgent`
(= UserPromptSubmit). These read the prompt and session state, not tool
schemas, so they work regardless of Gemini's tool naming.

**Deliberately NOT registered (pending live verification):**
`BeforeTool`/`AfterTool` → the exfil guard, output preprocessing, and any
tool-level feature. Reason: Gemini's tool NAMES (`run_shell_command`,
`read_file`, `replace`, …) and tool_input FIELD names (`absolute_path` vs
Claude's `file_path`, etc.) differ from Claude's and are unverified from
this environment. Wiring the exfil guard blind risks it reading an empty
path, silently passing, and giving false security. When Gemini's tool
schemas are confirmed on a real install, add a host-scoped tool-name +
field normalizer at the daemon entry (map to Claude canonical), register
`BeforeTool`/`AfterTool`, and the ask→deny/advise translation (already
built) carries the exfil guard and vuln-add prompts.

**Install:** `deadeye init gemini` writes a self-contained extension under
`~/.deadeye/gemini-extension/` (manifest + `hooks/hooks.json` + adapter
script) and prints `gemini extensions install --path …` — deadeye never
edits Gemini's own config; the install step is the user's consent. The
exact Gemini hooks.json shape and the `gemini extensions install --path`
flag are from the docs and unconfirmed live.
