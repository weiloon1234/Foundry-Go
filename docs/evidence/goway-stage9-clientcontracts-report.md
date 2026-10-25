# Stage 9 Lane B — clientcontracts

Package taken: **clientcontracts** (decision 12). The accepted upsert+D3 delta is
preserved. No shared package, catalogue, harness or generated file is edited.

## Migration

`contracts.go` now returns `orm.PageResult[httppagination.MemberResponse]` and
`orm.SimplePage[httppagination.MemberResponse]`; cursor mapping takes
`orm.CursorPage[mutatorqueries.Member]`. These aliases retain the existing
pagination metadata and DTO/accessor mapping without changing transport types.
There are no direct projection builders left in this package: reporting exports
already use the integrated `reporting.MemberRows`, `OrderRows`, `MemberTotals`
schemas and their ORM/rows datatable sources. The handwritten non-persistent
`Account` identity remains unchanged.

## Prerequisites

All requested packages exist: `genericdto`, `httppagination`, `httpuploads`,
`localization`, `models`, `mutatorqueries`, `reporting`, `requestflow`, `unions`
and `internal/clientfixture`. **The accepted httppagination ORM migration is not
in this base**: its database service still calls the generated member query.
Its public result aliases are compatible, so clientcontracts is ready for that
integration. No prerequisite files were copied or modified.

## Contracts and acceptance

The existing frozen-byte test exports both client and reporting surfaces and
compares the artifact set and every byte. Its manifest, OpenAPI, TypeScript,
React/Vue, validation, authentication and DTO assertions remain unchanged.
All 45 protected DTO/generated/testdata files retain their original SHA-256.
No declarations require regeneration. No rows ported, framework surface additions,
footprint-source changes or ceiling increases.

Build, vet, package tests/races, eight compiler negatives, catalogue and the
requested `make typescript-check` pass. Frozen contract-byte comparisons pass
for both client and reporting surfaces.

See [evidence](goway-stage9-clientcontracts.json) for exact targeted commands,
results, tool paths and source hashes. PostgreSQL and Redis remain disabled;
the native client gate uses only local HTTP/WebSocket servers.

## Docs notes

Use ORM pagination result aliases in client endpoint handlers, with explicit DTO
presentation and the existing rows-backed reporting declarations. The published
contract bytes and TypeScript sources stay unchanged. Integrate lane D's accepted
httppagination migration at the next barrier; no new framework capability is needed.
