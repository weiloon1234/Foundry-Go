# Security hardening acceptance — 2026-09-24

Accepted on 2026-09-24. Implementation, final native verification and the requested
re-audit/fix round are complete.
The [blueprint](../../blueprint/security-hardening/README.md) owns the requested scope;
the [evidence record](../evidence/security-hardening-20260924.json) retains commands,
source fingerprints, failures, corrections and verification limits.

| Item | Delivered behavior | Evidence owner |
| --- | --- | --- |
| S01 | Cookie authentication and browser sessions share a `no-store` default, including optional/anonymous paths and failures. Explicit downstream response policy remains application-owned. | [Real raw/typed authentication with ETag middleware](../../http/cookie_response_test.go) |
| G01 | Native CI, pinned actions, independent security scans, dependency update proposals, support policy and an enabled private reporting channel. | [Security policy](../../SECURITY.md), [scanner regression tests](../../tools/security_scan_test.py) |
| G02 | Typed scheme/host/port/CIDR restrictions check complete DNS answers and connect to approved IP literals. Restricted clients bypass proxies and reject custom transports. | [Outbound policy tests](../../httpclient/destination_test.go), [typed configured consumer](../../tests/fixtures/consumer/security/configuration_test.go) |
| G03 | Standard Webhooks HMAC/Ed25519 and Stripe signatures authenticate bounded original bytes, freshness, delivery identity and a concrete configured account before typed decoding or replay. | [Verifier tests](../../webhook/verifier_test.go), [real durable webhook consumer](../../tests/fixtures/consumer/idempotenthttp/publication_test.go) |
| G04 | Explicit nontransactional SQL retains an advisory lock, durable progress and operator reconciliation. Default transactional checksums are unchanged. | [Actual concurrent index and interrupted recovery](../../database/postgres/nontransactional_migrations_test.go), [unknown-outcome protocol regressions](../../database/migration_runner_test.go) |

## Re-audit and corrections

All five implementations were traced through their public consumer and failure paths.
The review corrected four additional findings:

1. Public-address classification now excludes Azure's platform WireServer address,
   which otherwise looks globally routable. Explicit CIDR configuration remains
   the deliberate way to permit an approved service.
2. An unfinished migration now rejects missing prerequisite history during status,
   resume and reconciliation. The regression simulates history loss only in the
   in-memory protocol fixture; it deletes no real database data.
3. Verifier formatting now redacts both pointers and copied values. Regressions
   assert a fixed label for ordinary formatting variants instead of merely looking
   for the original key text.
4. Stripe timestamp presence is tracked independently of its value, so an empty
   first timestamp cannot hide a duplicate. Regression permutations and a Stripe
   fuzz target exercise that valid-signature edge case.

The implementation also reuses the already-read migration history, strengthens
journal-name identity, preserves native typed CIDR configuration through generated
keys, and avoids duplicate editor/client runs in CI. Failed signature, body-limit,
body-read, cancellation and internal-clock paths have actual HTTP response tests.

## Verification

The final gate uses native Go, warm caches, existing PostgreSQL/Redis and required
editor/TypeScript modes. `make verify`, the full PostgreSQL/Redis race gate,
affected final-source HTTP/webhook and durable consumer races, and both protocol
fuzzers passed. The final fuzz round exercised 2,091,745 inputs. Compiler rejection
cases, real gopls, strict TypeScript HTTP/WebSocket transport, generation freshness,
formatting, vet, documentation and release/security tooling are included. All
3,661 runtime/test/client source files and 91 build/generation/workflow inputs
match the final verification fingerprints.

Security scans reported no affected functions and no npm vulnerabilities.
Module-only OpenPGP and editor-tool advisories remain recorded; this is not a claim
that the dependency graph contains no advisory entries. The final evidence retains
the scanner version, advisory database snapshot and each finding.

The workflow lives at `.github/workflows/verify.yml`; it was parsed and its embedded
shell checked locally. Hosted execution remains untested because no changes were
pushed. Private vulnerability reporting is enabled and was read back through GitHub.

Webhook provider formats are checked with local crypto vectors and actual database
idempotency, not live provider account certification. PostgreSQL concurrent indexes
and cancellation use the real server; lost-response cases use protocol fault
injection. Existing cloud/mail provider adapters were not changed or recertified.
No package was published and no production deployment was performed.
