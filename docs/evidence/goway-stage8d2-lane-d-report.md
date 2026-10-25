# Stage 8d2 — lane D behavior ports

Base: `e7b4d8a45fd40f77d680930758e859d92681425f`. No public framework API removed; no generated output or generator source edited.

## Assertion map

| Old anchor | Native anchor and retained assertions |
| --- | --- |
| `internal/extensiontest/fixture.go`: `query.Define`, `ForModel`, `IdentityOf`, `MemberQuery`, `MemberIDField` | ORM declarations and identities, `SoftDeleteOf`, `extensions.DefineORMOwner`; presentation retrieval is poisoned, ownership reads remain key-only. Natural string product keys and member soft-delete visibility remain covered by the dependent suites. |
| `extensions/owner_test.go`, `internal/extensionrow/identity_test.go`, `metadata/rescope_postgres_test.go`: generated owner declarations | Native owners; persisted scopes, pre-rename identities, pinned storage names, declared rescoping, batch limits, collision rejection and exact subject keys survive. |
| `translations/translations_paging_postgres_test.go`: field `.Matching`; `attachments/collections_postgres_test.go`: collection `.Matching`, `query.SelectValue` | Owner-typed `Matching` functions and `rows.ReadScalar`; exact counts/keys, long-text hash-index behavior, current soft-delete visibility, loaded snapshots and statement counts survive. The attachment collection edit is the small cross-owner dependency of the fixture migration. |
| `attachments/slot_postgres_test.go`: `DefineMany`, query `.With(...).Load` | `ManyOf`, concrete `SlotOf`/`BindSlot`, `orm.Load`; unopened/unbound rejection, accepted-file publication, missing-slot rejection, ordered loaded links and variant rejection survive. A separately registered concrete owner retains the declaration-identity check. |
| `attachments/handle_files_test.go`: legacy `AddFiles` comparison leg | Native `AddFilesTo` preflight still asserts `fault.Invalid`, nil results, zero file opens; attempted results, unknown publication and no retry remain executable. |
| `database/decoding_paths_test.go`: `Test*RedactsInBothQueryAPIs` | `Test*RedactsInORM`; JSON scalar and relation-batch failures retain redaction/cause identity, closed rows, no partial result and unchanged parent slots. Only legacy branches removed. |
| `database/orm/rows/result_{read,cursor,sensitivity}_test.go`; HTTP pagination tests; audit/notification tests; goway pgsmoke reads/result-cursor/audit | Native pagination aliases; every existing boundary, nullable/scalar sensitivity, cursor direction/scope, authentication, cancellation, HTTP status/body, lock and persisted-state assertion remains. |
| `goway/integrations/TestORMPaginationKeepsTransportIdentity` | Native transport types and parser outputs replay all nine original frozen observations. HTTP mapping and zero-cursor URL rejection remain live. |

HTTP modelbinding behavior tests already use frontend-independent resolvers/native foundations; low-level database driver tests already have no legacy query imports. They were audited without introducing frontend dependencies. HTTP binding and database race suites passed.

## Frozen evidence

No new capture was needed. The nine integration observations were already published from the legacy side at commit `c95f9606f6429ef6ae7214446a51fa81b307e626`, with source-manifest SHA-256 `26e4fc21824cdee840766c2ac17542a8f12f614085bf3bcc2d9712de008649c8`. `records.json` and `state.json` are untouched. Only the five changed fixture-source census hashes were refreshed after assertion review.

The existing root shared owner, `tests/oracle`, supplies format validation and replay to independent fixture modules. No format or validator was copied. Its new `Replay.Match` rejects capture mode and unpublished expectations; the inventory recognizes native replay sites and retains occurrence accounting. Legacy and native replay share the same comparison implementation. These are test-infrastructure helpers, not ORM surface additions.

## Remaining legacy callers

No active engine calls remain in the lane-owned tests/fixture helper. Production compatibility entry points remain deliberately intact for 8d3: `http/modelbinding/{field,nested}.go`, `extensions/owner_legacy.go`, and `attachments`, `translations`, `metadata` `matching_legacy.go`/`slot_legacy.go`. Audit (`model`, `retention`, `timeline`, `command`), `internal/auditstore/store.go`, and notification inbox still spell page/limit contracts through query aliases; they are not engine executions. The HTTP cursor scalar's published `database/query.Cursor[...]` string remains a wire identity, not a Go dependency.

No legacy-only invariant was left unported. Footprint probes/ceilings, ORM surface snapshots, catalogue rows and contracts are unchanged.

## Verification

Exact commands, source hashes, segmented-run failures and final outcomes are in `goway-stage8d2-lane-d.json`. PostgreSQL checks use the configured service in required mode and retain isolated schemas. Initial sandbox startup failures were rerun with local-service access; initial test-port compile errors were fixed before final acceptance.

## Docs notes

Stage 8d2 lane D ports extension ownership/matching/loading and remaining pagination/decoding behavior coverage to ORM without retiring public APIs. Preserve the published HTTP cursor scalar identity through 8d1/8d3. Lead-owned docs were not edited.
