<!-- deadeye-vapt: canonical rubric; edit internal/vapt/ruleset.md, the skill and every host rendering are generated from it -->
# Deadeye VAPT

A whole-service vulnerability assessment and penetration test, static and
whitebox: find this service's real attack surface, then find what an
attacker reaches through it. This is not a diff review. `/deadeye-guard`
and `/deadeye-review` catch what changed; this catches what's already
there — the endpoint nobody touched this session, the auth check that was
never written, the route still live from an API version everyone thinks
is retired.

**What this is not.** No traffic is sent, no exploit is run, nothing
outside this repository's source is touched — no network probing, no
host/container/cloud-IAM layer, no runtime fuzzing. This is the source
half of a VAPT: a whitebox read that reasons like an attacker with the
code in hand, not the network half. Say this plainly in the output, not
just here.

## Scope

`git ls-files` (or `find -maxdepth` outside a git repo) — never read
directories of files whole. Grep-first for candidates; open full file
bodies only for the ones that make the attack-surface table.

**Phase 0 — confirm it's a service.** Grep for route/handler/controller
registration shapes across the ecosystems present (`@app.route`,
`router.`, `http.HandleFunc`, `@RequestMapping`, `#[get(`, Express/Fastify
`app.get/post/...`, gRPC service defs, GraphQL resolvers). Nothing found
→ say so plainly and stop. Do not invent endpoints for a CLI, a library,
or a static site — that is the single most damaging false positive this
skill could produce.

**Phase 1 — attack-surface inventory.** Every route: method, path,
handler, and what auth middleware is actually mounted on it (not just
declared somewhere in the file — trace whether it's in the chain for
*this* route). File upload, webhooks, admin panels, GraphQL/gRPC/
WebSocket endpoints, background jobs and queue consumers. Include every
API version still routable, even a `/v1/` next to a `/v2/` — a live
deprecated version is itself a finding (`inventory:`, API9), not just
scope. Report this table first, before any finding — it's the artifact a
pen-test report opens with.

**Phase 2 — trust-boundary map.** Per surface, name which inputs are
attacker-controlled: path params, body, headers, cookies, query string,
uploaded content, third-party webhook payloads, queue messages. An input
with no name in this map cannot produce a finding later — every finding
traces back to a boundary named here. Working state, not printed —
same as Phase 3's ranking.

**Phase 3 — triage, then deep-read only the top candidates.** An
endpoint taking an object id with no visible ownership check outranks one
taking no input at all. Rank before you open files, not after.

**Phase 4 — verify.** Same proof discipline `/deadeye-review` and
`/deadeye-guard` enforce, raised to pen-test standard: a finding's
`proof:` is a **reproduction**, not a description — the concrete request
(method, path, a real param value) and the exact sink it reaches. Before
claiming an authz/validation/rate-limit check is MISSING, grep OUTSIDE
the obvious file and follow the value into the callee — middleware, a
base handler, a decorator one call up — the real guard often lives there.
No reachable input, no reproduction → drop the finding; a claim without a
proof is a guess with a CVSS score attached.

{{owasp}}

## Report format

One block per finding, richer than a diff-review one-liner because a
pen-test finding needs a reproduction and a remediation, but reusing the
same four severity glyphs the rest of this product speaks rather than
inventing a CVSS scale: 🔴 `critical` (exploitable now, data loss, account
takeover), 🟠 `high` (must fix before this ships), 🟡 `medium` (should
fix), ⚪ `nit` (optional / defense-in-depth).

```
🔴 authz: IDOR on GET /api/orders/{id}
   endpoint: orders.go:88 (handler getOrder)     owasp: API1:2023 / A01:2025
   link:     https://owasp.org/API-Security/editions/2023/en/0x11-t10/
   attack:   any authenticated user swaps {id} and reads another tenant's order
   proof:    id comes from mux.Vars(r)["id"] at L88, passed straight to
             db.GetOrder at L94; grep for ownerID/tenant across the package
             returns nothing on this path
   fix:      scope the query by the session's tenant, not the path param
```

`owasp:` names every category the finding maps to (Top 10 id, API Top 10
id, or `llm:LLM0N`) — never left blank; that's what the coverage matrix
below is built from. `link:` is mandatory on every finding: the matching
URL from the Reference table above (Top 10:2025 → the Top10 link, APIx →
the API Security link, LLMx → the LLM link; cite both when a finding maps
to two taxonomies). Always one of those three fixed URLs — never a
fabricated per-category deep link, and never omitted. `fix:` gets a code
snippet when the fix is mechanical, same rule `/deadeye-review`'s
"Suggested fixes" uses — a judgment-call fix ("which auth policy is
correct") stays prose.

**The report closes with a mandatory coverage matrix — nothing left
behind.** Every category from every applicable list above gets exactly
one line, one of three states:

- `findings: <ids>` — the findings under this category, by number
- `clean` — checked, nothing survived verification
- `n/a — <reason>` — e.g. `n/a — no LLM surface`, `n/a — no third-party
  API consumed`

A category may never be silently absent from the matrix. If the pass
could not actually reach a category (too large to triage fully, a
sub-surface never opened), say `not reached — <why>` — that is a
coverage gap to disclose, never a clean result to imply.

End with the tally (`<C> critical, <H> high, <M> medium, <N> nits`), the
coverage matrix, and the scope disclaimer from above (no traffic sent,
source-only). Nothing survives verification anywhere → skip individual
findings and print exactly:

`Clean line of fire — no service-level exposure survived verification.`

— still followed by the full coverage matrix; "clean" is a per-category
verdict, not a reason to omit the matrix.

## Honesty boundaries (load-bearing)

- Never invent a CVE, advisory id, or CVSS score you did not see from a
  tool or an OSV/advisory database lookup.
- If a dependency manifest exists, run its native auditor when installed
  (`govulncheck ./...`, `npm audit`, `pip-audit`, `cargo audit`, or
  `osv-scanner -L <manifest>` as a fallback) — no auditor installed → say
  so, do not fabricate a result. No manifest anywhere in the repo (a
  script or app with no lockfile/dependency file at all) → `dep:` is
  `n/a — no dependency manifest`, not `not reached`; `not reached` is
  reserved for a manifest that exists but the pass genuinely couldn't
  examine (too large, auditor timed out).
- State plainly, every report, that no traffic was sent and no exploit
  was run — this is a source read, not a live pen-test.
- Findings above the cap (~25) get ranked, not padded — say how many
  lower-severity ones were omitted; never present a ranked sample as
  exhaustive coverage.
- A `deadeye: <shortcut>. ceiling: <limit>. upgrade: <trigger>.` comment
  over a hunk is a recorded decision, not a finding — count it separately
  as accepted.

## Learning loop

Before finalizing, run `deadeye lessons priority` (best-effort — retry
once with `~/.deadeye/bin/deadeye` if `deadeye` isn't on PATH; if that
also fails, run the pass normally). Recent coder misses or disputed
findings on the `security:` lens raise the bar for the matching tag here
too — this rubric shares the learning signal with `/deadeye-guard` and
`/deadeye-review`'s security lens, not a separate ledger.

For each finding that survives verification and makes the final report:

```bash
deadeye lessons record coder-miss security:<tag>
```

using the tag without its trailing colon — e.g. an `authz:` finding →
`deadeye lessons record coder-miss security:authz`. No-op when coder mode
wasn't active this session.

When the user disputes a finding ("that's not reachable", "already
gated upstream", "won't fix"):

```bash
deadeye lessons record review-false-positive security:<tag>
```

## Boundaries

- Findings are a LIST. Do not apply or push any fix unless asked.
- This is a static, source-only, whitebox pass — never imply live traffic
  was sent or an exploit was run.
- Not a diff review: `/deadeye-review`/`/deadeye-pr` (four lenses,
  diff-scoped) and `/deadeye-guard` (security-only, diff-scoped) are the
  right tool for "did this change introduce a problem." This is "what can
  an attacker already reach in this service," scoped to the whole repo by
  design.
- No route/handler surface found → say so and stop. Do not review a CLI,
  library, or static site as if it were a network service.
