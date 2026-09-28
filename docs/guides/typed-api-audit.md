# Typed API implementation audit

T07 reviews the T01–T06 changed implementation and its complete consumer workflow.
The initial native `make verify` gate passed before the review. The grouped fixes
below passed focused native race and real-client checks, independent packaged
consumers and controlled measurements. Eight bounded fuzz targets passed; the final
full gate passed in 633.4 seconds. The [acceptance record](../evidence/typed-api-t07.json)
contains commands, review notes, samples, boundaries and resulting source hashes.

## Review coverage

The review covered every handwritten implementation file in the combined T01–T06
acceptance inventories: 109 framework/build files, plus the three embedded
TypeScript HTTP/wire/metadata runtimes. It also covered the handwritten consumer
integrations and all T07 workflow, subprocess, client and benchmark sources.
Generated artifacts are covered through their generator owners, deterministic
regeneration and executable consumers; they are not claimed as individually
hand-reviewed source. Earlier milestone records retain their original hashes.

| Boundary | Reviewed behavior |
| --- | --- |
| Generic DTOs and unions | Source/type identity, concrete substitution, byte slices, codecs, schema collisions, finite graph expansion, closed variants and owned payloads |
| Requests and responses | Input-source cardinality, safe decode paths, preparation/validation/policy order, concrete actors, supplied/null/value states, callback ownership and response publication |
| Model binding | Named selectors, alternate unique fields, direct parent relationship constraints, duplicate detection and current resource policy |
| Database testing | Explicit complete named/default scopes, schema reset on checkout, read-pool alignment, bounded connections, migration ownership, startup cleanup and retained data |
| Idempotency and delivery | Framed caller/operation/key scope, canonical input, atomic claim/business/result/outbox, rollback, primary reconciliation after ambiguous commits, bounded work, replay and receiving receipts |
| Client/tooling integration | Manifest/OpenAPI/TypeScript agreement, strict presence and numeric contracts, required typed keys, external test inputs, generation recovery and compiler/editor boundaries |

The integrated receiving example commits its unique delivery receipt and counter
in the same named database transaction. Its guarantee covers that local database
effect. An arbitrary external service still needs its own idempotency contract.
The existing T06 process tests terminate owned processes before and after commit;
T07 additionally proves complete-workflow replay from a fresh process.

## Findings and fixes

1. **Named generic byte slices:** the emitter specialized only unnamed slices.
   `Items[T] []T` instantiated with `byte` could describe an array while Go emitted
   base64. The existing `JSONSliceType` path now inspects the underlying slice and
   preserves its named type. Generated round trips and combined generic/concrete
   schema export exercise byte and non-byte specializations.
2. **TypeScript node budgets:** parsing and writing omitted object names from
   `Nodes`. Both now count names, matching Go. Real generated-client tests check
   exact limits for empty, ordinary, nested and tagged objects in both directions.
3. **External test inputs:** the source fingerprint omitted runtime `.mjs` files.
   Those files now invalidate child acceptance. A regression changes content
   without changing size or timestamps, while timestamp-only edits stay stable.
4. **Shared fixture authentication:** the workflow imported the entire idempotency
   application just to reuse an actor and guard. Both now use one private loopback
   identity fixture. Public fixture aliases keep existing callers working.
5. **PATCH presence:** the handler maintained a second boolean beside the generated
   draft state. It now uses `ProjectDraft.IsEmpty()`; existing persisted omitted,
   null, empty, zero and replacement cases cover the behavior.

No new dependency or duplicate framework subsystem was introduced by these fixes.
The [workflow guide](typed-api-workflow.md) explains the public consumer path;
status and final evidence belong to the
[master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery).
