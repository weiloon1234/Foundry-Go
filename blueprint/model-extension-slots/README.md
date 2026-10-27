# Model extension slots

Native model slots are ordinary Go fields whose policies live in explicit typed
handles. Stage 8 retires the former generated binding layer. The storage owners
remain [translations](../../docs/guides/model-extensions.md#translated-model-fields),
[attachments](../../docs/guides/attachments.md) and
[metadata](../../docs/guides/model-extensions.md#typed-metadata).

The [master](../00-master-architecture-and-parity.md#model-extension-slot-delivery)
owns status. Earlier E01–E05 documents preserve the original design and acceptance
history; use the [current guide](../../docs/guides/model-extensions.md) and
[articles consumer](../../tests/fixtures/consumer/articles/tables.go) for native APIs.

## Current consumer shape

`orm.Define` declares stored columns and `orm.SlotOf` declares external slots.
`extensions.DefineORMOwner` retains explicit owner/storage names and a validated
`orm.IdentityOf`. `extensions.BindSlot` connects the typed key and slot;
`translations.TextOf`, `attachments.OneOf`/`ManyOf` and `metadata.ValueOf` declare
policy-bearing handles. Bind managers once with `.From(manager)`; pass
`.Relation(slot)` to `Query.With` or to the query used by `LoadFrom`/`LoadMissingFrom`.

Registration and cleanup use native extension declarations. Request DTOs map to
handles explicitly, preserving concrete keys, transaction membership and slot
policy. Slots do not become SQL columns or implicit public DTO fields.

## Existing contracts to preserve

- Owner identity: owner name plus storage model form the persisted scope; the
  [rename rules](../../docs/guides/model-extensions.md#owner-identity-and-table-renames),
  natural-key and bulk-delete rules apply to native owners.
- Bounds, admission, cancellation and callback ownership of the extension store
  and each manager. Load batches stay within existing owner, row and byte limits.
- Translation semantics: canonical `i18n.LocaleID`, catalog membership, empty text
  as a present value, fallback order and retention of removed locales.
- Attachment publication, recovery, variants, active-content refusal, detected
  media and `Result.Publication`. No attachment value serializes implicitly.
- Metadata versioning and strict decoding; incompatible stored versions fail.
- Relation loading limits, loaded/empty/absent states and no hidden I/O on reads.
- Typed assignments target persisted columns. Slots are never assignment fields.

## Scope and ownership

- Framework only, with independent consumer acceptance fixtures; no starter.
- Package layering stays acyclic: `extensions` ← `translations`, `attachments`,
  `metadata` ← `extensions/slots` ← `application`. The ORM imports none of
  them; shared eager loading accepts validated native slot relations instead.
- No new wire schema. Localized input uses the existing `map[i18n.LocaleID]string`
  DTO support; uploads use `foundryhttp.UploadedFile`; metadata values use
  generated DTO descriptors. Persistence models do not become public DTOs.
- PostgreSQL only. Existing named/default services and application ownership
  remain the assembly contract.

## Implementation cadence

Finish each milestone's implementation, regression sources, consumer examples and
documentation before compiling/testing. Then run its consolidated verification
round, collect failures, batch fixes and repeat affected checks until the final
source passes its required gate. Reuse the existing compiler, generation and
editor batching.

Every implementation milestone requires `make verify`, relevant native PostgreSQL
integration and races, deterministic generation, public consumer acceptance and
the applicable compiler/TypeScript/gopls checks. Review consumer ergonomics before
proceeding. E05 additionally requires one complete re-audit of the series,
fixes and final-source verification.

This blueprint delivery itself uses documentation/link checks and source review
only. Implementation gates must not be reported as run merely because the plan
names them.
