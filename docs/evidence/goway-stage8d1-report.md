# Stage 8d1: shared contract owners

Base: `5d8d3fe3`. No public entry point removed. Temporary aliases and the two
legacy bridges retire in 8d3 under the program's `patches/stage8d-decisions.md`.

## Moves and preserved contracts

- `query.MorphName` / `query.Morphable` now alias `model.MorphName` /
  `model.Morphable`; ORM aliases that same owner.
- `query.RelationLimits` and `orm.RelationLimits` alias the existing
  `querycore.RelationLimits`. Defaults and validation delegate to that owner.
- Page/cursor production signatures and parsing in audit, notifications, the
  archive and audit store use ORM's existing aliases. Application maintenance,
  audit limits and factory bounds use querycore directly. No limit was retuned.
- Fixture morph declarations use model; the retained JSON Snapshot uses the
  existing database.JSONKind owner. Six fixture test changes replace only alias
  or constant spellings; paired assertions remain. Their census hashes were refreshed.
  Ten further native-only page/cursor fixture sources (negative cases, footprint
  probes and a lint input) now use ORM aliases; diagnostics and budgets are unchanged.

Before moving declarations, 390 tracked JSON/TypeScript/generated/golden
artifacts (excluding historical evidence) were hashed. No recorded origin for
MorphName, Morphable or RelationLimits was found in those artifacts, contract
transcripts or diagnostic snapshots. The only current Go API snapshot occurrence
was explicitly approved by the lead:

```diff
-  MorphName func() github.com/weiloon1234/Foundry-Go/database/query.MorphName
+  MorphName func() github.com/weiloon1234/Foundry-Go/model.MorphName
```

Aliasing RelationLimits also exposes its owner's existing `Validate func() error`
for value and pointer receivers; both entries are recorded in public-methods.txt.
The existing cursor wire descriptor at `http/pagination/cursor_request.go:33`
intentionally retains `database/query.Cursor[...]`; its byte identity and test
remain untouched. The lead was asked to confirm this pre-existing descriptor as
an exception to the broad origin stop instruction. It is unrelated to the moved
relation types.

## Consumer limits

Ten new public constants, all aliases of querycore constants, live only in ORM:
`MaxInsertRows`, `MaxIdentityBatch`, `DefaultChunkSize`, `MaxCursorBytes`,
`MaxCursorFields`, `MaxExpressionDepth`, `MaxExpressionNodes`, `MaxParameters`,
`MaxMembershipValues`, `MaxScalarSQLBytes`. Existing `MaxPerModelWriteRows`,
`MaxRelationWriteRows`, `MaxChunkSize` and `MaxPageSize` remain their sole native
spellings. The identity test covers all fourteen and rejects another native
constant with the same name. Consumer batch, cursor and iteration tests now use
them. The ORM README documents the limits and shared statement budgets.
Plan decoder bounds and lifecycle callback recursion limits have no application
input/configuration contract; they remain internal rather than gaining new API.

## Dependency guard and remaining callers

The guard scans ORM and all internal production packages, including build-tagged
files. The only exceptions are the exact `LegacyIdentityScope` function in
`identity_legacy.go` and `Extension` in `extension_legacy.go`. Additional
query-dependent declarations in those files fail too. Exceptions are documented
“retire in 8d3” with the decisions reference. Negatives cover new imports in ORM,
expr, rows and existing/future internal owners, widened allowed files and hidden
imports. Stale exceptions fail.

[The exhaustive caller census](goway-stage8d1-callers.json) lists every actual
Go import and its selected query symbols, including compile-fail and footprint
sources. C-class production callers remain in:

- `attachments/{matching,slot}_legacy.go`, `metadata/{matching,slot}_legacy.go`,
  `translations/{matching,slot}_legacy.go`: predicate and extension-slot adapters.
- `extensions/owner_legacy.go`: model identities and predicate scopes.
- `database/prune/legacy.go`, `testkit/factory/factory.go`: legacy Query,
  ModelQuery/CreateDraft/Mutation construction.
- `http/modelbinding/{field,nested}.go`: KeyField, ModelQuerySource, Unique,
  RelatedUnique and ScopedRelation.
- The two allowlisted ORM bridges: Predicate/FoundryPredicate and
  ExtensionSlot/FoundryExtension (plus the shared limits alias).

C-class root behavior callers remain in `database/legacy_adapter_error_test.go`
and `database/orm/adjustment_lifecycle_test.go`. Retained compiler/footprint
fixtures are enumerated individually in the census. Transition-only alias tests
are classified separately. D-class implementation remains package-local in
`database/query`; its full production file inventory is in the same census.
No native internal production package imports the legacy engine.

## Acceptance

[Acceptance evidence](goway-stage8d1.json) records source hashes, commands and
results. Build/vet, all touched-package races, the convertibility and alias
identity guards, fixture-check (required PostgreSQL, compiler cases, frozen
replay/census and footprint), catalogue, generate-check, docs-check, gofmt and
diff checks passed. One legacy identity expectation was updated for the approved
RelationLimits relocation; the affected query package race rerun passed.

All 389 protected generated/contract/golden artifacts in the pre-move hash set
remain byte-identical. Only the reviewed fixture census metadata changed. No
footprint ceiling or frozen oracle transcript changed.

## Docs notes

For the lead-owned changelog: native limits now support bounded application
batches without importing the legacy query package; relation contracts have one
shared owner. Legacy aliases remain only until the already approved 8d3 removal.
