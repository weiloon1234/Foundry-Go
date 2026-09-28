# T02 — Discriminated payload unions

Prerequisite: T01. Status belongs to the
[master](../00-master-architecture-and-parity.md#typed-api-delivery).

## Problem and wire contract

Before T02, [schema kinds](../../contract/schema.go) represented objects and scalar
enums without payload unions. A custom decoder alone cannot give runtime validation, OpenAPI and
TypeScript a shared closed set of payload shapes. Add a first-class tagged union
for requests, responses and nested DTO fields.

The initial representation is an internally tagged JSON object with an explicit
string discriminator. Example wire values for one proposed `PaymentMethod`:

```json
{"kind":"card","token":"provider-token"}
```

```json
{"kind":"bank_transfer","reference":"transfer-reference"}
```

The union declaration owns the discriminator name, unique literal tag and concrete
DTO for each variant. Each variant DTO owns its ordinary payload fields. Neither
handwritten TypeScript nor a second handwritten JSON schema lists those fields.
Reject a payload field colliding with the discriminator, duplicate tags, empty
variant lists and conflicting generated names during generation/assembly.

## Go consumer contract

Generate a concrete union value, typed constructors for each declared variant,
typed accessors and a complete visitor/match helper. Constructors accept their
concrete DTO; accessors never return `any` or require a consumer type assertion.
The ordinary valid construction path selects one variant. The zero/unset union is
invalid unless wrapped in the existing Optional/Nullable types. Ordinary Go does
not promise compiler-exhaustive arbitrary switches; the generated complete visitor
must require every variant callback when that guarantee is desired.

Use explicit generator declarations following existing directive conventions;
write a complete public consumer fixture to settle the exact declaration and
generated symbol names before implementation spreads. Generated storage and codecs
are implementation-owned. Do not expose a map-backed variant bag or require users
to implement JSON dispatch. Support concrete instantiated generic variant DTOs
from T01. The initial variant payload is an object; untagged, externally tagged and
arbitrary scalar unions are outside this milestone.

## Concrete source contract

The implemented declaration uses `//foundry:union name=PaymentMethod discriminator=kind` on a
typed `PaymentMethodVariants` struct. Its `union:"card"` fields list concrete DTO
types. Generation owns the separate `PaymentMethod`, constructors returning
`(PaymentMethod, error)`, typed accessors, discriminator constants and the complete
`MatchPaymentMethod[R]` function. See the [consumer guide](../../docs/guides/tagged-unions.md)
and [public fixture](../../tests/fixtures/consumer/unions/payment.go).

Generated storage captures an immutable validated wire snapshot using the same
ownership approach as `value.JSON`; typed accessors decode fresh values. This
prevents mutation aliases and recursive custom encoder chains. Construction and
accessor costs must be recorded alongside response costs. Local recursive union
schemas use graph edges without recursive descriptor factories. Manifest format
2 makes the added closed-union shape explicit. The [native acceptance evidence](../../docs/evidence/typed-api-t02.json) records
verification, batched fixes, compiler/editor/client acceptance and measured costs.

## One graph and codec pipeline

Extend the shared contract graph with discriminator/variant edges and teach its
normalizer, compiler, validation, owned snapshots and serialized manifest about the
new node. Reuse existing JSON scanning, duplicate-key detection, Unicode checks,
depth/node/step/issue limits and field path reporting. Reject unknown/missing tags,
incorrect tag types, mixed-variant fields and invalid variant payloads. Input cannot
select an undeclared Go implementation. Encoding must validate the selected variant
before writing response headers; invalid output remains an internal error.

Generate OpenAPI `oneOf` and a discriminator mapping using literal-tag constraints
on each variant. TypeScript uses a discriminated union with precise payload types,
including optional/null and lossless numeric representations. Runtime TS encode/
decode and static types must agree. Nested union error paths refer to wire fields,
not private generated storage names. Version the manifest format if required;
older readers must reject unsupported shapes instead of treating them as dynamic.

Do not silently publish new variants as backwards compatible for exhaustive older
clients. Document producer/consumer rollout and unknown-tag policy. Initial strict
clients reject unknown tags; an explicit future open-union mode is separate scope.

## Acceptance

- Real Go/TypeScript HTTP round trips for every variant, nested unions, collections,
  generic envelopes and optional/null union fields.
- Compiler-negative cases for wrong constructor payloads, unrelated union types,
  wrong handlers and incomplete typed visitor calls; real gopls branch completion.
- Generator/schema failures for duplicate tags, field collisions and invalid
  variants; deterministic output and cross-package identities.
- Runtime/fuzz rejection of missing/unknown/duplicate tags, mixed shapes, deep
  recursion and malformed payloads, with bounded work and safe diagnostics.
- Output/manifest/OpenAPI/TS parity, older-client rejection and source ownership
  checks. Existing scalar enums/custom JSON codecs retain their behavior.
- Benchmarks compare small/large variant sets with an equivalent handwritten codec;
  measure allocations, output size and generated build/editor cost.

Complete implementation/test/docs before the consolidated verification/fix gate.
