---
name: deadeye-vapt
description: Whole-service VAPT / pen-test pass -- complete OWASP Top 10:2025, API Security Top 10 2023, and LLM Top 10:2025 coverage, ranked worst-first.
license: MIT
---

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

**Phase 0 — confirm there's a real attack surface.** Four independent
tracks; any one alone is enough to proceed, a target may have any
combination:

- **Network-facing.** Grep for route/handler/controller shapes
  (`@app.route`, `router.`, `http.HandleFunc`, `@RequestMapping`,
  `#[get(`, Express `app.get/post`, gRPC/GraphQL defs).
- **LLM/agent context-injection.** Grep for an LLM SDK call (`anthropic`,
  `openai`, `.messages.create(`, `.chat.completions.`, `generateContent(`,
  a langchain/llamaindex import), a prompt/system-message template built
  from variables, or an agent/hook framework injecting content into a
  model's context (`additionalContext`, a `SessionStart`-shaped hook, a
  RAG/tool-output pass-through). A CLI with no HTTP surface can still be
  exactly this — its whole job is feeding external content to an LLM.
- **Message/event-driven.** Grep for a queue/topic consumer or
  subscriber registration (`kafka.NewConsumer`/`consumer.Subscribe(`/
  `@KafkaListener`, `channel.Consume(`/`@RabbitListener`, SQS
  `ReceiveMessage(`/`@SqsListener`, `redis...Subscribe(`, NATS
  `nc.Subscribe(`) or a scheduled/cron handler that processes external
  data on a trigger instead of a request. No inbound HTTP request and no
  LLM call doesn't mean no attacker-controlled input — the message body
  is exactly that.
- **Client-side/UI.** Grep for a frontend framework (React/Vue/Angular/
  Svelte components, a client-side router — `createBrowserRouter`,
  `<Route path=`, `useNavigate`) or a browser-extension manifest. A
  UI-only repo with no backend at all still renders untrusted content,
  stores tokens, and embeds third-party scripts — it's a real target on
  its own, not a "nothing to review here."

None found → say so and stop. Whichever track(s) DO apply set Phase 1's
inventory shape (below); tags with no matching surface end up `n/a` in
the coverage matrix, not skipped from it.

**Phase 1 — attack-surface inventory.** Network-facing: every route,
method, path, handler, and what auth middleware is actually mounted on
it (trace the chain, not just what's declared in the file). Uploads,
webhooks, admin panels, GraphQL/gRPC/WebSocket endpoints. Every API
version still routable — a live `/v1/` beside a `/v2/` is itself a
finding (`inventory:`, API9), not just scope.

LLM/agent surface: every place external or repo-derived content reaches
the model's context — a hook injection point, a RAG result, a
tool-output pass-through, a file scan rendered into a prompt — and
whether that text carries ANY framing marking it untrusted. No framing
IS the finding (`llm:`, LLM01) — no crafted payload needs to be
demonstrated; a missing trust boundary is reportable the same way a
missing authz check is, without a full exploit chain.

Message/event-driven surface: every queue/topic consumed, the consumer
group, what triggers processing, and whether the payload is
schema-validated and the claimed sender authenticated BEFORE use — most
consumers trust whatever arrives. Note redelivery/retry behavior: an
unbounded retry on a poison-pill message is a `ratelimit:`/`dos:`
finding on its own, not just an inconvenience.

Client-side/UI surface: every place user- or third-party-controlled
content renders into the DOM, where auth tokens are stored (localStorage
vs. an httpOnly cookie), every `postMessage` listener and whether it
checks `event.origin`, third-party script/widget embeds, and whether a
CSP exists at all.

Report the applicable table(s) first, before any finding.

**Phase 2 — trust-boundary map.** Per surface, name attacker-controlled
inputs: path params, body, headers, cookies, query string, uploads,
webhook payloads — LLM surface: repo/file content, tool output,
retrieval results — message/event surface: message body,
headers/metadata, claimed sender identity — client-side/UI surface:
URL/query string, `postMessage` payloads, third-party script content,
anything a server response reflects into the DOM. Unnamed here, no
finding later. Working state, not printed — same as Phase 3's ranking.

**Phase 3 — triage, then deep-read only the top candidates.** An
endpoint taking an object id with no visible ownership check outranks one
taking no input at all. Rank before you open files, not after.

**Phase 4 — verify.** Same proof discipline `/deadeye-review` and
`/deadeye-guard` enforce, raised to pen-test standard: a finding's
`proof:` is a **reproduction**, not a description — the concrete request
(method, path, a real param value) and the exact sink it reaches, or —
for an LLM surface — the payload shape that would reach the model's
context unframed and the injection point it flows through. Before
claiming an authz/validation/rate-limit check is MISSING, grep
OUTSIDE the obvious file and follow the value into the callee —
middleware, a base handler, a decorator one call up — the real guard
often lives there. No reachable input, no reproduction → drop the
finding; a claim without a proof is a guess with a CVSS score attached.

## Coverage — every OWASP category, mapped to a tag

Eighteen tags. Every OWASP Top 10:2025 category, every API Security Top
10 2023 category, and every LLM Top 10:2025 category maps to one — none
dropped, none silently folded away.

**Reference — cite these, never a fabricated deep link:**

| taxonomy | link |
|---|---|
| OWASP Top 10:2025 (A0x) | https://owasp.org/Top10/2025/ |
| OWASP API Security Top 10 2023 (APIx) | https://owasp.org/API-Security/editions/2023/en/0x11-t10/ |
| OWASP Top 10 for LLM Applications 2025 (LLMx) | https://genai.owasp.org/llm-top-10/ |

**OWASP Top 10:2025** (the current edition — supersedes 2021; two
categories are genuinely new, and two 2021 categories were folded into
others rather than carried forward unchanged, noted below):

| id | category | tag |
|---|---|---|
| A01:2025 | Broken Access Control (absorbs 2021's standalone SSRF category) | `authz:` (SSRF shape still separately tagged `ssrf:`) |
| A02:2025 | Security Misconfiguration | `config:` |
| A03:2025 | Software Supply Chain Failures (expands 2021's "Vulnerable and Outdated Components" to the whole ecosystem, not just direct deps) | `dep:` |
| A04:2025 | Cryptographic Failures | `crypto:` |
| A05:2025 | Injection | `inject:` |
| A06:2025 | Insecure Design | `bizlogic:` |
| A07:2025 | Authentication Failures | `authn:` |
| A08:2025 | Software or Data Integrity Failures | `integrity:` |
| A09:2025 | Security Logging & Alerting Failures | `logging:` |
| A10:2025 | Mishandling of Exceptional Conditions (new for 2025 — error handling and logic errors) | `exceptions:` |

**OWASP API Security Top 10 2023:**

| id | category | tag |
|---|---|---|
| API1 | Broken Object Level Authorization | `authz:` |
| API2 | Broken Authentication | `authn:` |
| API3 | Broken Object Property Level Authorization | `massassign:` / `expose:` |
| API4 | Unrestricted Resource Consumption | `ratelimit:` |
| API5 | Broken Function Level Authorization | `authz:` |
| API6 | Unrestricted Access to Sensitive Business Flows | `bizlogic:` |
| API7 | Server-Side Request Forgery | `ssrf:` |
| API8 | Security Misconfiguration | `config:` |
| API9 | Improper Inventory Management | `inventory:` |
| API10 | Unsafe Consumption of APIs | `thirdparty:` |

**OWASP LLM Top 10 2025** — apply only when the service has an LLM or
agent surface; state `n/a — no LLM surface` in the coverage matrix
otherwise:

| id | category | id | category |
|---|---|---|---|
| LLM01 | Prompt Injection | LLM06 | Excessive Agency |
| LLM02 | Sensitive Information Disclosure | LLM07 | System Prompt Leakage |
| LLM03 | Supply Chain | LLM08 | Vector/Embedding Weaknesses |
| LLM04 | Data and Model Poisoning | LLM09 | Misinformation |
| LLM05 | Improper Output Handling | LLM10 | Unbounded Consumption |

All ten fold under `llm:`, with the sub-id named in the finding
(`llm:LLM01`) — except LLM03, which is a `dep:` finding wearing an LLM
hat (a poisoned or unpinned model/plugin/tool dependency), and LLM10,
which is `ratelimit:` (unbounded token/cost consumption).

## The eighteen tags

| tag | covers |
|---|---|
| `authn:` | absent/weak authentication, JWT signature unverified, `alg:none`, `kid`/JWK header injection, algorithm confusion, no expiry, session fixation, weak reset/OTP flow, non-constant-time credential compare, OAuth `state`/PKCE/`redirect_uri` flaws |
| `authz:` | BOLA/IDOR, BFLA, missing tenant scoping, privilege escalation, CSRF, path-based access-control bypass, GraphQL field-level authz |
| `bizlogic:` | insecure design: abuse-control-free business flows, race/TOCTOU on balance or inventory, negative/overflow quantities, workflow step skipping, no threat-model-driven limits |
| `inject:` | SQL, NoSQL, command, LDAP, XPath, SSTI, CRLF/header, path traversal, zip-slip, XSS sinks, unsafe deserialization, XXE, prototype pollution |
| `ssrf:` | attacker-controlled URL reaching a fetch, cloud metadata/internal network reachable, webhook and redirect-follow fetches, DNS-rebind-prone validation |
| `massassign:` | request body bound straight to a model, letting a client set `role`, `is_admin`, `balance`, `verified` |
| `expose:` | excessive data in a response on the NORMAL path (PII, hashes, internal ids, over-broad fields), secrets in logs, debug endpoints reachable, XS-leaks |
| `validation:` | absent/weak boundary validation — no schema, type confusion, unbounded size, content-type confusion, missing allow-list |
| `ratelimit:` | no throttle or quota on login, OTP, reset, signup, expensive query, or any resource-creating endpoint; ReDoS; unbounded pagination; GraphQL alias/batch amplification |
| `crypto:` | weak/absent crypto, secrets stored plaintext, ECB/static IV, non-CSPRNG token, TLS verification off or weak version |
| `config:` | debug mode on, permissive CORS, missing security headers, insecure cookie flags, directory listing, GraphQL introspection, default credentials, TRACE, clickjacking, host-header injection, cache poisoning/deception, request smuggling, WebSocket origin unchecked, gRPC reflection |
| `dep:` | vulnerable or superseded dependency, unpinned CI action ref, mutable `:latest` base image, `curl \| sh` installer, LLM03 supply chain |
| `integrity:` | unsigned/unverified update or plugin load, CI/CD pipeline trusting unreviewed input, subdomain takeover -- the SUPPLY-CHAIN/trust dimension; a deserializer that executes attacker-controlled code is `inject:`, not this |
| `logging:` | auth failures and privileged actions with no audit trail, monitoring blind spots, log injection/forging |
| `inventory:` | undocumented/shadow endpoints, deprecated API versions still routable, non-prod or debug hosts exposed, orphaned routes |
| `thirdparty:` | third-party API responses trusted without validation, unvalidated redirects to partner services, blind trust in upstream data shape |
| `llm:` | prompt injection, system-prompt leakage, improper output handling, excessive agency, embedding/vector weaknesses, model/data poisoning, misinformation, unbounded consumption |
| `exceptions:` | mishandled exceptional conditions — an uncaught exception leaking a stack trace or internal state (the ERROR-path counterpart to `expose:`'s normal-path over-sharing), a caught error that fails open on a security-relevant path, a logic error in error-recovery code, a resource left in an inconsistent state after a partial failure |

**Overlap rule** — three pairs above share a mechanism at a glance; tag
by the more specific one and never split one finding across two matrix
lines: a deserializer that executes attacker-controlled code is
`inject:` (not `integrity:`, which owns the supply-chain/trust dimension
— an unsigned update, a poisoned CI input, a takeover); an exception path
that leaks a trace or internal state is `exceptions:` (not `expose:`,
which owns normal-path over-sharing — a response returning more fields
than the caller needs); a privilege decision that trusts a self-declared
identity or role with no server-side check is `authz:` (not `authn:` —
the exploitable defect is the untrusted decision, and it exists even in
a system with solid identity proof elsewhere). Report `authn:` as its
own separate finding only when missing identity verification is
independently exploitable on its own path; otherwise name it once in
`proof:` alongside the `authz:` finding it enables, same as the other
two pairs — never split one finding across two matrix lines.

**Citation scope** — don't assume every tag dual-cites; cite whichever
table above actually carries a row for it. `authz:`, `authn:`,
`bizlogic:`, and `config:` have a real row in BOTH tables — cite both.
`dep:`, `crypto:`, `inject:`, `integrity:`, `logging:`, and
`exceptions:` have a Top 10:2025 row only. `ssrf:`, `massassign:`,
`expose:`, `ratelimit:`, `inventory:`, and `thirdparty:` have an API
Security row only (`ssrf:` is credited inside A01:2025's own entry
above, not a dedicated `ssrf:` row — cite API7 alone, not A01). `llm:`
always cites the LLM table regardless of the other two. `validation:`
has no dedicated row anywhere in any of the three tables — cite the Top
10:2025 link generically and name the ASVS chapter (V5, below) in
`fix:` instead of forcing a citation that doesn't exist.

**Beyond the Top 10** — classic pen-test findings with no standalone
Top-10 slot fold into the tags above rather than get dropped: CSRF and
clickjacking (`authz:`/`config:`), open redirect and host-header
injection (`config:`), HTTP request smuggling and web cache
poisoning/deception (`config:`), subdomain takeover (`integrity:`),
TOCTOU race conditions and negative-quantity abuse (`bizlogic:`), ReDoS
and pagination/batch amplification (`ratelimit:`), zip-slip and
file-upload-to-RCE (`inject:`), prototype pollution (`inject:`), JWT
`kid`/JWK confusion and OAuth/SAML flow flaws (`authn:`), GraphQL
batching and field-level authz (`ratelimit:`/`authz:`), gRPC reflection
and WebSocket origin checks (`config:`). Each one names its owning tag in
the finding line, so the mapping stays explicit rather than implied.

**ASVS** (OWASP Application Security Verification Standard) is the depth
reference per tag — when a finding needs a stricter control statement
than "this is wrong," name the relevant ASVS chapter (V2 Authentication,
V4 Access Control, V5 Validation, V8 Data Protection, V10 Malicious Code,
V13 API) in the fix. Referenced per-finding, never enumerated wholesale —
350+ controls inlined would bury the rubric a pen-tester needs to scan
fast.

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
- None of Phase 0's four tracks found → say so and stop. Do not invent
  a route, an LLM call, a consumer, or a rendered page for a plain
  library that genuinely has none of the four.
