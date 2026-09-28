# Security hardening continuation

Implementation and verification are accepted in the [master](../00-master-architecture-and-parity.md).
The [acceptance report](../../docs/guides/security-hardening-20260924.md) links
current-source evidence and the completed re-audit corrections.

Implement the [2026-09-23 review](../../docs/guides/security-gap-review-20260923.md)
as one acceptance scope. The [master](../00-master-architecture-and-parity.md) owns
status. S01 and G01–G04 are all required; conditional priorities in the review do
not remove them from this user-authorized implementation.

| Item | Required behavior | Evidence |
| --- | --- | --- |
| S01 | Shared no-store default for cookie auth, optional/anonymous routes and failures | Real raw/typed handlers; browser-session and cache middleware regressions |
| G01 | Repository-owned native verification and independent security scans; actionable private disclosure and support policy | Workflow and scanner tests, existing required-backend/tool gates, confirmed reporting channel |
| G02 | Opt-in typed outbound host/scheme/port/network policy, resolved-address enforcement, direct connections and immutable configuration | DNS rebinding/mixed records, literal IPv4/IPv6, proxies, rejected custom transports, approved internal endpoint; public consumer |
| G03 | Reusable provider signature verification over bounded original bytes, rotation, timestamp tolerance, typed account/delivery identity | Standard Webhooks/provider vectors, tampering/freshness/duplicates, actual consumer idempotency without duplicate effects |
| G04 | Explicit nontransactional PostgreSQL migrations with locked progress and reconciliation | Real concurrent index, interruption/uncertainty recovery, unchanged transactional checksums, public consumer |

Complete the source, regression tests, consumer usage and guides before the
verification/fix loop. Reuse Makefile commands and warm caches. Run final native
verification, affected races, real PostgreSQL/Redis, generated/compiler/editor
contracts and relevant TypeScript transport acceptance. Re-audit all implemented
items once, fix findings together and verify the final source.

Do not introduce a new ORM, service locator, queue, idempotency store or crypto
primitive. Keep direct infrastructure composition and configured typed application
assembly usable. Migrations must never reset/drop existing databases or silently
retry statements with unknown outcomes. CI authoring does not authorize Git
commits, pushing or release publication.
