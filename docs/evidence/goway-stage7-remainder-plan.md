# Stage 7 additive remainder after batch 6b

Base: `f3cfffd`. This replaces the pre-integration remainder inventory. Slice 3a
already owns HTTP pagination, model binding, factories, identity/extension slots,
and unique/related lookup adapters. Lane B's typed declaration roles are integrated.
No files in those implementations or their ORM surface snapshot are changed here.

| Item | Dependencies and available capability | Catalogue and contracts | Disposition |
| --- | --- | --- | --- |
| Application validation (D7) | Existing `orm.CheckedTable` / `orm.Check`; no legacy query dependency | New `application_table_declaration` negative; configuration and provider startup ordering | Add `application.WithTables`, validate before constructors, snapshot the list, preserve instance isolation. Convert thin `.Table` handles inside the model package and return an owned interface slice. |
| Seeders | `seed.Definition` supplies an existing transaction; `Table.Insert` supports ordinary assignments and defaults | No seeder-specific catalogue row; IDs, transaction outcomes, defaults and SQL remain unchanged | Add ORM recipe, old/new SQL parity and PostgreSQL source. Runtime needs no migration. |
| Pagination/cursor consumers | 3a supplies all page/cursor aliases, parser and HTTP adapters | Four `http_cursor_*_owner` and ten `http_pagination_*` rows; cursor bytes, reflected identities, response DTO and authorization contracts | Keep additive consumers, transport regression tests and 14 catalogue ports. Do not restore nine HTTP edits or duplicate cursor API/snapshot entries. |
| Model binding, factories, extension adapters | Integrated 3a `Keyed`, `Unique`, `RelatedUnique`, identity/slot adapters, `factory.Define` and `InsertEach` | 3a's separate 14 catalogue ports and runtime contracts | Already owned by lane D; no missing-foundation claim and no edits. |
| Settings and remaining stores | Settings still uses extensionstore; lane D owns five remaining store migrations and their callers | Settings names/version/JSON, persistence and cache semantics | Leave to lane D; no competing store declaration. |
| Database validation | Existing validation code uses legacy `ValueLookup`, batched SQL equality and trusted key exclusion. 3a supplies model unique lookup, not the complete value/batch adapter | `validation_batch_id`, `validation_database_{id,locked,owner,value}`; rule specs, messages and batching | Separate remainder work; this re-application does not introduce a lookup/compiler API. |
| Auth/reference callers | 3a now provides model identity and extension owner adapters; remaining caller migration must use those | Typed subject/reference identity and authorization contracts | Reassess callers after lane D's store integration; not a wholly missing identity foundation. |
| Datatables | Lane C owns migration and contract exports | `datatable_*` rows and byte-identical manifest/OpenAPI/TS | Excluded. |
| Pruning, importer fixtures and generator retirement | Separate 6e parity and Stage 8/9 work; importer callbacks and seed runtime are already model-generator independent | Their existing catalogue dispositions remain untouched | Excluded from this additive re-application. |

The 14 ports are `http_cursor_mapping_owner`, `http_cursor_navigation_owner`,
`http_cursor_request_owner`, `http_cursor_result_owner`,
`http_pagination_auth_authorization`, `http_pagination_auth_cursor`,
`http_pagination_auth_numbered`, `http_pagination_auth_permission`,
`http_pagination_auth_scopes`, `http_pagination_auth_simple`,
`http_pagination_handler`, `http_pagination_map_owner`,
`http_pagination_simple_result` and `http_pagination_simple_total`.

## Implementation and acceptance

1. Restore only application validation, consumers, regressions, catalogue rows and
   guides. Adapt declarations to the integrated roles; regenerate the response DTO
   and manifest using the Makefile.
2. Split the former combined archive probe (11,483 functions) into registration,
   pagination and seeding packages. Keep option-only and application-only controls;
   use a model-owned interface-slice boundary for registration. Measure
   each archive and set new ceilings to exactly ceil(measured × 1.15); preserve all
   existing ceilings.
3. Run the requested build, scoped vet/race, fixture vet/test, catalogue and format
   acceptance with the r4 cache and four-process limit. Compile/vet PostgreSQL
   sources without running PostgreSQL. Record current source hashes and results.

No new Query/Table methods, capabilities, dependencies or duplicate SQL owners.
Lead-owned documentation changes belong in the report's Docs notes.
