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

**Overlap rule** — two pairs above share a mechanism at a glance; tag by
the more specific one and never split one finding across two matrix
lines: a deserializer that executes attacker-controlled code is
`inject:` (not `integrity:`, which owns the supply-chain/trust dimension
— an unsigned update, a poisoned CI input, a takeover); an exception path
that leaks a trace or internal state is `exceptions:` (not `expose:`,
which owns normal-path over-sharing — a response returning more fields
than the caller needs). If the finding also touches the other tag's
dimension, name it once in `proof:` rather than duplicating the finding.

**Citation scope** — most tags map to both a Top 10:2025 category and an
API Security category, so `link:` cites both per the report-format rule
above. Four tags are API-only by design (no Top 10:2025 counterpart
exists for their category): `massassign:`, `ratelimit:`, `inventory:`,
`thirdparty:`. Cite only the API Security link for these — there is
nothing to dual-cite.

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
