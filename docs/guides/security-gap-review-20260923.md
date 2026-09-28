# Security and capability review — 2026-09-23

Historical review: S01 and G01–G04 were subsequently implemented and accepted on
2026-09-24. See the [follow-up acceptance](security-hardening-20260924.md). The
findings and evidence below preserve what was true at the original review.

Foundry-Go has **one confirmed cookie-response cache-safety gap** in the reviewed paths. Four additional priorities depend on release and application requirements. They are separated below from vulnerabilities and from capabilities already delivered. This is a review record; runtime behavior has not been changed.

The [evidence record](../evidence/security-gap-review-20260923.json) contains the reproducible probe source, scanner findings, commands, test results, source fingerprints and limitations.

## S01 — cookie-authenticated responses lack a safe cache default (P2)

[NewCookieAuthentication](../../http/guard_binding.go) installs origin/CSRF protection, but its [shared authentication middleware](../../http/authentication.go) does not set a response cache policy. The separate [BrowserSessions wrapper](../../http/browser_sessions.go) already sets `Cache-Control: no-store`. The [bootstrap consumer](../../tests/fixtures/consumer/bootstrap/auth.go) demonstrates the affected cookie-authentication entry point.

A public-API probe registered raw and typed JSON profile routes and made two valid cookie-authenticated requests to each. Both routes returned the correct account-specific bodies without a cache policy. The raw route produced:

| Response | Status / body | Cache-Control | Vary | Last-Modified |
| --- | --- | --- | --- | --- |
| Account 1 | `200 / account-1` | absent | `Sec-Fetch-Site`, `Origin` | present |
| Account 2 | `200 / account-2` | absent | `Sec-Fetch-Site`, `Origin` | present |

The typed route likewise omitted `Cache-Control` and cookie-dependent `Vary`, without setting `Last-Modified`. The raw handler deliberately supplies `Last-Modified` to demonstrate an ordinary cacheable representation. A shared intermediary that caches these cookie requests can reuse one person's body for another. HTTP permits heuristic freshness in applicable cases; cookies do not themselves establish the required response cache policy. See [RFC 9111, sections 4.2.2 and 7.3](https://www.rfc-editor.org/rfc/rfc9111.html).

**Confirmed:** the missing protection on this supported path. **Conditional:** cross-user disclosure needs a cache that stores such responses; no production proxy was tested. This is not an authentication bypass. The normal BrowserSessions path already applies the safe default.

Recommended correction: put a `no-store` default at the shared cookie-authentication response boundary, including optional authentication and early failures, while preserving intentional explicit response policy through a reviewed contract. Reuse the policy for both entry points. Regression coverage should verify both required and optional cookie routes, distinct actors, failures and interaction with cache middleware. Adding `Vary: Cookie` alone is not an equivalent ban on retaining sensitive responses.

## Important missing capabilities

### G01 — repository-owned continuous verification and security reporting

**Priority: before public releases.** The [Makefile](../../Makefile) provides substantial verification, and [release measurement](../../tools/measure-release.py) optionally runs vulnerability scans. No checked-in CI workflow or `SECURITY.md` was found in the current repository. External automation and hosting settings were not inspected, so their absence is not asserted.

Reuse existing verification targets in CI, require the configured backend/client/editor checks, and run dependency analysis independently of expensive cold measurements. Examine scanner findings rather than relying only on its exit status. Publish supported-version and private-reporting instructions using confirmed maintainer details. This is an operational assurance gap, not evidence that current code is exploitable.

### G02 — a safe outbound mode for application-supplied URLs

**Priority: before fetching URLs supplied by untrusted users.** The generic [HTTP client URL path](../../httpclient/url.go) accepts absolute HTTP(S) destinations. Its [transport](../../internal/httptransport/transport.go) uses ordinary dialing and environment proxies, without an address policy. A probe successfully contacted an actual loopback test server. Named clients with `BaseURL` correctly reject a different absolute origin; redirects are not followed.

General connectivity is useful for internal services and is not itself an SSRF defect. The missing facility is an opt-in typed policy for applications such as URL previews, importers and webhook destination registration. Validate approved schemes, ports, hosts and the resolved addresses used for each connection; account for IPv4/IPv6, DNS changes and proxy behavior. Preserve explicitly permitted private services. This follows the destination-validation concerns in [OWASP's SSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html).

Acceptance should include denied loopback/private/link-local destinations, DNS changing to a denied address, proxy behavior and approved internal endpoints. Checking only the input hostname would leave the central risk unresolved.

### G03 — reusable inbound webhook verification

**Priority: before consuming provider webhooks.** [Idempotency documentation](idempotent-operations.md) correctly requires a verified provider/account/delivery identity before claim or replay. The [consumer webhook test](../../tests/fixtures/consumer/idempotenthttp/publication_test.go) implements its own small HMAC adapter. No reusable production verifier is present.

Provide bounded original-body handling, provider-specific signature verification, rotating verification secrets, signed timestamp tolerance and a typed verified delivery identity. Then reuse existing idempotency and outbox facilities. A valid signature and freshness window establish authenticity and freshness; idempotency separately prevents duplicate effects. [Standard Webhooks](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md) provides a concrete interoperable reference for signing delivery ID, timestamp and original payload.

Acceptance should cover body/header tampering, expired timestamps, rotated keys, valid repeated deliveries and account scoping. A signed URL alone does not authenticate a webhook body.

### G04 — online/nontransactional PostgreSQL migrations

**Priority: before schema changes on large live tables that must keep accepting writes.** The [PostgreSQL runner](../../database/migrate/postgres.go) wraps every migration's statements and history insertion in one transaction. The [migration guide](migrations-and-seeding.md) explicitly excludes operations that cannot run there.

Consequently, the runner cannot execute `CREATE INDEX CONCURRENTLY`. PostgreSQL documents that concurrent index builds run outside transaction blocks and can leave an invalid index after failure. See [CREATE INDEX](https://www.postgresql.org/docs/current/sql-createindex.html).

Add an explicit execution mode while retaining session locking, recorded progress, failure/uncertain outcomes and operator reconciliation. Nontransactional SQL and its history row cannot be presented as one atomic commit. Acceptance should cover successful concurrent creation and interrupted recovery; do not automatically drop an existing index or blindly retry an uncertain migration.

## Capabilities already delivered

The earlier broad gap list does not describe current Foundry-Go accurately. Current contracts and acceptance cover:

- [Handler-connected typed API contracts](typed-api-workflow.md), generic DTOs/tagged unions, generated clients and response/error/status metadata.
- [Omitted/null/value semantics](typed-values.md), typed PATCH inputs, and [request validation](http-requests.md) across JSON, query, form and multipart paths.
- [Named, nested and scoped model binding](scoped-model-binding.md) and [isolated PostgreSQL HTTP tests](isolated-http-tests.md).
- [Inbound idempotency](idempotent-operations.md), including current authorization on replay, and a [durable transactional outbox](outbox.md).
- [Authentication workflows](auth-operations.md) and [trusted public host/origin policy](http-public-urls.md).

Redis Cluster/Sentinel and federated identity/passkeys can be future deployment/product choices. They are not universal prerequisites for the existing scope. PostgreSQL-only support remains a coherent design choice.

## Review coverage and verification

All **65 current module families** were inventoried against the preceding [module review](module-review-20260923.md). All **3,643 runtime/generator/client source entries** in its verified snapshot still match. The new inventory also fingerprints the three existing release-tool Go files excluded from that earlier snapshot. Dependency/build inputs have separate current fingerprints.

This pass concentrated fresh source tracing on attack and failure boundaries:

| Area | Reviewed boundaries |
| --- | --- |
| Bootstrap, foundation, configuration, infrastructure | Configuration ownership, named defaults, provider lifecycle and application isolation |
| Auth, HTTP, WebSocket | Credential sources, current authorization, CSRF/origins, proxy/public URLs, cookies, body/response bounds and upgrade admission |
| HTTP client, cloud, storage, attachments | Destination handling, TLS/transport policy, key/path confinement, transfers and attachment ownership |
| Database, models, datatables, metadata/settings/translations | Query boundaries, scoped data, persistence, migrations and tenant/resource identity |
| Jobs, events, outbox, idempotency, notifications | Commit outcomes, replay authorization, durable publication, retry and delivery identity |
| Redis, cache, leases, rate limits, pubsub, schedules | Shared-state bounds, isolated keys, coordination ownership and cancellation |
| Encryption, secrets, tokens, sanitization, imaging, email | Credential/data boundaries, unsafe input and bounded external content |
| Contracts, validation, OpenAPI, TypeScript, generation | Typed wire semantics, hostile object keys, generated ownership and declaration consistency |
| Logging, faults, tracing, audit, health, diagnostics, maintenance, observability | Safe diagnostics, cardinality/admission bounds and callback ownership |
| CLI, plugins, testkit, tooling and value/support packages | Explicit trust boundaries, test isolation, developer tooling and previously verified value contracts |

The remaining low-risk value/support paths retain the unchanged prior acceptance evidence. This is not a new line-by-line review of every file or a deployment penetration test.

Selected tests finished with **53 distinct packages passing**. Seven additional packages contain no tests. The first persistence batch failed only the Redis package because its test address was absent; selecting the observed, already-running service and rerunning `./redis/...` passed. No service was started and no store/database was reset. Two Redis helper packages reused cached results on that correction; the actual Redis integration package ran.

Real PostgreSQL and Redis integration ran. The live AWS and R2 certification tests were skipped because provider configuration is absent. There was no new full build, race/fuzz campaign, editor probe or TypeScript runtime run: unchanged runtime/generated/client source retains the separate full acceptance baseline.

Fresh scans used the installed `govulncheck v1.8.0`, native `go1.27.1`, and the reported vulnerability database snapshot `2026-09-16T18:00:43Z`:

- Framework and independent consumer: only module-level `GO-2026-5932` for `x/crypto`; no affected package/function trace. The advisory concerns OpenPGP, which the reviewed source does not use.
- Release tool: no findings.
- Existing gopls binary: three module-level advisories (`GO-2026-6179`, `GO-2026-6180`, `GO-2026-5970`); no affected symbols reported for this binary. Keep them in tooling upgrade review.
- TypeScript tooling lockfile audit: zero vulnerabilities.

Scanner evidence is bounded by the selected graph and advisory snapshot; it is not a proof that all code or deployed configurations are safe. See [Go vulnerability management](https://go.dev/doc/security/vuln/). The evidence record retains the initial failure, successful correction and final documentation/source checks. **S01 and G01–G04 remain open recommendations.**
