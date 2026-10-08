# 19 — Datatables and reporting

## Purpose and prerequisites

Prerequisites: [06](06-relations-and-advanced-queries.md), [08](08-http-validation-and-responses.md), [10](10-model-first-authentication-and-authorization.md), [11](11-storage-reliability.md). Reproduce server-side datatables without weakening query typing or duplicating response definitions.

Rust references: `src/datatable`, `src/database/projection.rs`, `tests/datatable_acceptance.rs`, `docs/guides/datatable.md`; Starter's `src/portals/admin/datatables`.

## Public contracts

A table descriptor binds table ID, row DTO/projection, typed columns, supported filters/orderings, authorization and export options. Client strings are decoded into this declared allowlist at the transport boundary; they are never passed through as SQL identifiers.

Delivered query shape:

```go
page, err := usersTable.Query(ctx, manager, authority, request)
```

The table's row type is fixed by its descriptor. Joined/computed display columns point to explicit typed expressions; their filter source is declared once. Column labels use translation keys and feed the shared contract manifest.

The [implementation guide](../docs/guides/datatable.md) records concrete contracts
and resource ownership. `projection dto=true` reuses generated JSON metadata and
typed validation selectors alongside SQL projection mapping. The manager borrows
the database and locale catalog; exact declaration registration is required.
`i18n.MessageKey` supplies the minimum label identity for this prerequisite of
milestone 20. `Description` supplies the manifest contribution consumed by
milestone 21; no independent TypeScript schema generator is introduced here.

## Implementation slices

1. Table/column registration and typed row projection integration.
2. Validated filter/sort parsing, pagination, count queries and deterministic order.
3. Subject/resource policy integration and mandatory server-side row scopes shared by list, count and export.
4. Streamed CSV and XLSX exports with bounded memory, size/concurrency limits and safe filenames. Reuse an existing spreadsheet library if present; necessary non-duplicating dependencies are preauthorized.
5. Table inspection metadata, TypeScript contract contribution, and example consumer table.

## Failure behavior

The 2026-10-08 request-classification correction reuses `http.BadRequest` for
client-input rejection while retaining `fault.Invalid` inspection. Declaration,
configuration, source and output-limit failures remain server errors. This fixes
consumer handlers that otherwise had to treat every invalid fault as client
input; the authenticated reporting query fixture returns errors unchanged.
The [datatable guide](../docs/guides/datatable.md) owns the exact runtime contract.

Reject unknown columns, unsupported operators and invalid scalar values before SQL. Prevent expensive unbounded requests with row/filter/depth limits. Exports must apply exactly the same authorization and soft-delete scope as interactive queries.

Protect spreadsheet output from formula injection under a documented escaping policy. Report streaming/export failures without leaving files marked complete; close database and storage streams on cancellation. Long-running export jobs reuse the job system rather than creating ad hoc background goroutines.

## Acceptance

Compile-test wrong row/projection/column ownership. Test malicious sort/filter input, joined fields, enum/nullable/decimal filters, stable pagination, scoped counts, unauthorized export, CSV escaping/formula injection, XLSX limits, cancellation and query-count/memory bounds. Apply the [common gate](README.md#common-completion-gate).


## Delivered behavior and verification

Typed declarations, generated report DTO projections, strict bounded requests,
scoped pages/counts/exports, WHERE/HAVING and relation filters, joined/grouped
reports, CSV/XLSX artifacts and existing HTTP/jobs/storage adapters passed native
milestone 19 verification. The [recorded evidence](00-master-architecture-and-parity.md#milestone-19-verification-and-consumer-review)
includes consumer, compiler/editor/generation, concurrency, fuzz, resource bounds
and corrections. Locale catalogs remain milestone 20 and the shared manifest
emitter remains milestone 21; their minimum typed contribution contracts exist.
