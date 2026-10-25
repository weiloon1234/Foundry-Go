# Stage 9 lane D: final catalogue rows

Base: `0ada5ac7`. Cache: r12; `GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.
No commit. No runtime adapter was needed: the existing ORM keyed lookup,
modelbinding and identity adapters already preserve the required types.

## Rows

Ten new compiler cases port the original restrictions:

- `auth_bound_resource_model`, `auth_bound_subject_model`: independently reject
  the wrong resource and subject after `BindAuthenticated` consumes an ORM keyed
  resolver.
- `auth_optional_bound_subject`: optional authentication still requires an
  `Optional[Member]` subject, even with a concrete ORM-bound resource.
- `http_model_binding_handler`, `http_model_binding_owner`,
  `http_model_binding_response`: retain the resolver owner, handler input and
  transport response through `ByKey(orm.Keyed(...))` and `Bind`.
- `reference_key_type`, `reference_model_owner`, `reference_natural_key`:
  `IdentityOf` / `ReferenceOf` retain the model-owned ID, independent reference
  owner and named string natural key.
- `workflow_bound_model`: `ByKey` plus `ThroughRelation` supplies a parent/child
  bundle; a bound workflow handler accepting only its child is rejected. The
  retained consumer case also checks the actual `Service.Patch` signature.

`importer_column_row` is **unaffected**, not obsolete. Its existing compiler
negative rejects an `OrderRow` callback in a `Column[MemberImport]` collection;
these are ordinary DTO types. Lane C's handoff identified the overly restrictive
area/stage pairing. Framework integrations now allow stage `—`, with regressions
proving that generated models, queries and projections still cannot be classified
as unaffected. The old compiler program and diagnostic remain unchanged.

The catalogue now has **0 pending**, 587 ported and 445 unaffected rows (1,032
rows total); 579 distinct Go-way cases cover the ported rows. No runtime-only
substitutions or obsolete dispositions were introduced.

## Acceptance

Exact commands, results and source hashes are recorded in
`goway-stage9-d-final-catalogue.json`. Targeted build/vet, the ten new compiler
cases, the nested-binding and identity races, and all eleven retained original
negatives pass. All ten corrected type variants compile through temporary
overlays without changing the tracked negatives. Catalogue validation passes
with zero pending; docs-check, gofmt and diff-check pass.

The Python suite passes seven of eight tests, including the new classification
regression. Only `test_complete_reviewed_catalogue_and_summary` fails because the
lead-owned README still has the previous totals. Its assertion is preserved;
the required replacement summary is included below.

## Surface, footprint and shared files

No runtime API, method surface, dependency, generated file, schema, codec,
manifest, OpenAPI or TypeScript change. No footprint inputs or ceilings changed;
new negatives live under compiler-only testdata.

Shared files touched: `blueprint/goway-data-layer/compilefail-mapping.tsv`
(only these eleven rows), `tests/fixtures/goway/compiler_cases_test.go`,
`tools/goway_catalogue.py`, and `tools/goway_catalogue_test.py`.
Other changes are the ten compiler programs and this slice's evidence.

## Docs notes

The lead should replace the catalogue summary in
`blueprint/goway-data-layer/README.md` with the validator output below, and update
its batch status from 577 ported / 11 pending / 444 unaffected to
587 ported / 0 pending / 445 unaffected. Stage 7 has 80 affected rows; the ordinary
DTO importer rule moves to unaffected. The compiler registry gains ten cases
(882 total); the 150 footprint probes and their ceilings remain unchanged.
This resolves the only expected Python suite failure without weakening the test.

| Disposition | Cases |
| --- | ---: |
| ported | 587 |
| port | 0 |
| merge | 0 |
| obsolete | 0 |
| unaffected | 445 |
| **Total** | **1032** |

| Area | Cases |
| --- | ---: |
| Authentication and security | 94 |
| Bulk writes and conflict policies | 61 |
| Cache, Redis and coordination | 61 |
| Core reads, writes and codecs | 74 |
| Events, jobs and messaging | 54 |
| Expressions and aggregates | 70 |
| Framework integrations | 81 |
| HTTP, DTOs and transport contracts | 112 |
| Joins and correlation | 101 |
| Lifecycle, mutators and observers | 39 |
| Other framework services and contracts | 83 |
| Relations and pivots | 25 |
| Rows, CTEs and sets | 77 |
| Validation, values and localization | 40 |
| Windows and specialized expressions | 60 |

| Stage | Cases |
| --- | ---: |
| 2a | 21 |
| 3 | 60 |
| 4 | 23 |
| 5 | 39 |
| 6a | 70 |
| 6b | 100 |
| 6c | 73 |
| 6d | 60 |
| 6e | 61 |
| 7 | 80 |
| — | 445 |

587 old catalogue rows are covered by 579 distinct Go-way cases.
