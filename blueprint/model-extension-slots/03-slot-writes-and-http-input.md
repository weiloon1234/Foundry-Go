# E03 — Slot writes and HTTP input

Prerequisites: E01–E02. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Existing behavior and additions

The managers already write through typed declarations and owner references:
atomic multi-assignment translation upserts with `SetIn` joining a business
transaction ([translations](../../translations/write.go)), versioned metadata
`SetIn` ([metadata](../../metadata/values.go)), and recoverable attachment
publication ([attachments](../../attachments/write.go)). Request DTOs already
decode `map[i18n.LocaleID]string` as a JSON object, and multipart forms carry it
as a JSON part (`form:"title,json"`). Uploaded files arrive as
[`foundryhttp.UploadedFile`](../../http/uploaded_file.go). Nothing turns an
uploaded file into an attachment upload, and no rule checks map keys against the
runtime locale catalog.

E03 adds write methods on bound descriptors that take the model value, plus
validation rules derived from slot policy, so request validation and persistence
share one source of truth.

## Translated text

Bound `TextSlot` methods derive the owner reference through the generated
`FoundryReference`:

- `SaveIn(ctx, tx, owner, map[i18n.LocaleID]string)` merges: it upserts the
  supplied locales and leaves others unchanged.
- `SyncIn(ctx, tx, owner, map[i18n.LocaleID]string)` makes the supported locales
  of this field exactly the input. Locales removed from the catalog keep their
  retained rows, as today.
- `ForgetIn` removes one locale; `ClearIn` removes the field.
- `Save`, `Sync`, `Forget` and `Clear` run in the manager's own transaction.

One call validates every entry before writing, then uses one set-based upsert,
plus one delete for synchronization. It reuses the existing text validation,
catalog membership, `MaxBytes` and assignment limits. Empty text is a present
value. `SyncIn` enforces `Options.Require`; merge writes cannot prove the complete
set and do not. A parent rollback rolls back the write. An uncertain commit is
never retried automatically.

`translations.SetIn` remains the atomic multi-field path. A bound helper that
writes several translated slots in one statement is added only if a consumer
fixture shows real need.

## Metadata values

Bound `ValueSlot` methods `SaveIn`, `Save`, `ForgetIn` and `Forget` reuse
`Key.SetIn` and `Key.Forget` with the declared version and contract. `ForgetIn`
adds the missing transaction-joined removal to the existing metadata owner, as
`ForgetIn` and `ClearIn` do for translations. Adding
optional fields to `V` needs no migration. Removing or renaming a field requires
a version increment, because strict decoding rejects stored unknown fields.
Freeform values use `metadata.Value[json.RawMessage]`. Values that must be
filtered or ordered belong in a typed column or a `value.JSON[T]` column instead.

## Attachments

Attachments remain outside the model transaction. A create flow commits the model,
its translations and metadata first, then publishes files; the existing
`Result.Publication` states whether each file was published, unpublished or
uncertain.

- `attachments.FileSource` is a small interface with `Open(context.Context)
  (io.ReadSeekCloser, error)`, `Name() string` and `ClientContentType() string`.
  `foundryhttp.UploadedFile` already satisfies it, so `attachments` does not import
  `http`. Detected bytes stay authoritative; the client type is only the existing hint.
- Bound `OneSlot` and `ManySlot` methods: `ReplaceFile(ctx, owner, file)`,
  `AddFile` for `Many`, and the `Upload` forms `Replace`/`Add`, plus `Detach`,
  `Clear` and `Reorder` taking the model value. The framework opens and closes the
  reader within the call; request cleanup still owns the spooled file.
- `attachments.AddFiles(ctx, slot, owner, files)` publishes files in order and stops at the first
  failure. It returns every result so far and never claims all-or-nothing
  publication. Atomic multi-file replacement is out of scope.

## Validation derived from slots

- `TextSlot.Rule()` returns a validation rule for `map[i18n.LocaleID]string`. It
  takes one catalog snapshot through `i18n.SnapshotLocales` from the borrowed
  catalog, then checks supported keys, `Options.Require` and the same text
  validation as the write. Issues use entry paths such as `/body/title/ms` through
  the existing [dynamic rules](../../validation/dynamic.go) and
  [map validation](../../validation/maps.go). A new issue code needs server and
  generated-client message recipes.
- File policy is exposed as `Accepts(ctx, file) (bool, error)` on attachment
  slots rather than a slot-owned rule. It reuses the attachment media detection
  and accepted-type check over the upload's bytes, after the filename and size
  checks. It never uses HTTP's generic sniff, which would reject OOXML documents
  that attachments accept. Applications wrap it in `validation.Custom` with their
  own rule ID, because one validator tree rejects several definitions of one
  custom ID; file counts use `validation.MaxItems`. The write remains
  authoritative, including image transformation limits.

## Wire contracts

Localized text needs no new DTO type. Omitted-field PATCH semantics use
`value.Optional[map[i18n.LocaleID]string]`. Multipart part names stay flat, so
per-locale files such as `images[en]` remain out of scope.

Generated TypeScript renders these maps as
`Readonly<Partial<Record<string, string>>>`. The contract manifest already records
configured locales ([manifest document](../../contract/manifest/document.go)). A
supported-locale union narrowing `i18n.LocaleID` keys in
[TypeScript](../../typescript/render.go) and OpenAPI was not delivered in this
series: it changes client and manifest compatibility and needs its own
[compatibility](../../docs/compatibility.md) review. The server rule is
authoritative.

## Acceptance

- PostgreSQL: create and update flows; merge versus synchronization; empty text
  preserved; unsupported locale and missing required locale rejected by both the
  rule and the write; parent rollback of translation and metadata writes;
  metadata version mismatch.
- Attachments after commit: published and unpublished results and `AddFiles`
  partial publication. Uncertain publication, request cancellation and spooled-file
  cleanup are the existing attachment and multipart contracts, which slot writes
  reuse unchanged and whose suites cover them.
- Real HTTP JSON and multipart endpoints with 422 paths; the file rule agrees with
  write-time detection for OOXML, SVG and plain-text cases.
- Strict generated TypeScript round trip for localized maps, and locale unions if
  that slice is delivered.
- Compile-fail: another model passed to a bound write; a wrong value type for a
  metadata slot; a rule applied to an incompatible field.

Finish all implementation, documentation and tests, then execute the common
milestone gate.
