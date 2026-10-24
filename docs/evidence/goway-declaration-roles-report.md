# Typed declaration roles

- CreatedAt/UpdatedAt/SoftDelete private-role aliases retain read methods; constructors return these roles.
- Primary[M,K] and PrimaryOf; NumberPrimary/TextPrimary and NumberLookupKey/TextLookupKey preserve key roles. PrimaryOf is only required for insert-select primary mapping and excludes InputKeyOf.
- SoftDeletable[M] via SoftDeletes; every force-delete path requires the concrete token and validates exact finalized declaration ownership.
- InsertTarget[M,V,P] and UpdateTarget[M,V,P] constrain MapInsert/MapUpdate with private layouts and existing sealed expression methods.
- All mutated fields require Input* carriers, including same-type inputs. Input capability families have distinct private storage fields.

Eight fixture declaration files and Countries migrated. The other seven Stage 7 stores have no affected managed pairs, mutators or soft-delete declarations. In total, 27 existing fixture files changed; 54 compiler negatives and 8 footprint probes were added.

Rows ported: `insert_select_mutator`, `insert_select_timestamp`, `source_update_mutator`, `source_update_timestamp`, `source_update_primary`, `source_delete_force`. `soft_force_key` retains its wrong-key rejection with a valid soft token.

## Footprint

Actual HEAD versus final T/t symbol counts. Existing ceilings are unchanged. Unchanged probes other than the two tight probes are listed in the JSON evidence.

| Probe | HEAD | Final | Ceiling |
|---|---:|---:|---:|
| aggregate_families | 4366 | 4366 | 4369 |
| audit | 1313 | 1337 | 1510 |
| conflict | 944 | 968 | 1086 |
| framework_countries | 4126 | 4157 | 4741 |
| lifecycle_changes | 1069 | 1093 | 1208 |
| lifecycle_write | 949 | 973 | 1070 |
| mutator_explicit | 1698 | 1717 | 1846 |
| q_with | 1211 | 1211 | 1211 |
| role_input_same | new | 1360 | 1564 |
| role_lookup_input | new | 1226 | 1410 |
| role_primary_number | new | 1195 | 1375 |
| role_primary_text | new | 1216 | 1399 |
| role_soft_read | new | 1548 | 1781 |
| role_soft_write | new | 620 | 713 |
| role_source | new | 905 | 1041 |
| role_time_read | new | 1394 | 1604 |

All 101 probes pass, including the model-growth and read-path audit-isolation checks. New ceilings are ceil(measured × 1.15). Write/mutator probe growth accompanies the required Input carriers; Countries now exposes distinct timestamp roles. The other 87 existing probes have no growth.

## Surface

New types: Primary, NumberPrimary, TextPrimary, NumberLookupKey, TextLookupKey, SoftDeletable, InsertTarget, UpdateTarget. New functions: PrimaryOf and SoftDeletes. No new Query/Table/column method names. The snapshot records 185 added lines and 10 approved signature replacements.

## Acceptance

Exact commands, prior failures and successful targeted reruns are retained in [JSON evidence](goway-declaration-roles.json). The segmented gate covers build, database/internal vet, the requested race packages and stores, fixture vet, 642 compiler negatives, 17 goldens, footprint, PostgreSQL source compilation, catalogue, generate-check, docs-check, gofmt and diff-check. PostgreSQL was not run.

## Docs notes

- Managed timestamp pairs migrate Time to CreatedAt/UpdatedAt and their Of constructors; lone unmanaged timestamps keep TimeOf.
- Expose SoftDeletable as a normal table field, e.g. Users.Soft. ForceDelete(ctx, db, q, Users.Soft, key, id); ForceDeleteUsing(q, Users.Soft, source); ForceDetach also requires the pivot token.
- Only insert-select primary mapping needs PrimaryOf. Table.Key remains sufficient for ordinary lookups and writes. NumberKey/TextKey retain lookup/primary roles; KeyColumn erasure does not restore source-write eligibility.
- Ordinary columns with explicit or discovered mutators now fail startup and name the field and required Input carrier. Use Input* even for I == V; password fields use InputColumn/NullableInputColumn with password.Codec().
- MapInsert/MapUpdate reject mutated inputs and UpdatedAt. CreatedAt remains eligible, matching the old shared runtime validator. Updates reject all primary/lookup roles.
- Lane A: remove convertibility allowlist entries (both directions, required and nullable) for InputColumn with InputNumber/InputText/InputTime/InputEnum/InputInterval/InputJSON, and InputEnum with InputNumber/InputText. Interval and JSON entries include mixed generic arities. No change to transaction_phase_test.go.
- No persisted schema, manifest, OpenAPI or TypeScript change; no new Query/Table/column method names. All eight Stage 7 stores reviewed; Countries is the only store with affected declarations at this base.

## Shared files touched

The complete list and SHA-256 source hashes are in the JSON evidence. Cross-owner files include `database/orm/relation_write.go`, the public-surface test/snapshot, fixture compiler/footprint registries, the catalogue mapping, Countries and fixture declarations. Lead-owned documents and Lane A’s convertibility guard were not edited.
