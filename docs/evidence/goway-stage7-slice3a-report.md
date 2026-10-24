# Stage 7 slice 3a: declaration integration adapters

Built on the accepted slices 1/2 at base `5cbe31d`. No commit. No stores or
persisted model shapes change in this slice.

## API and behavior

- Extension declarations use `orm.SlotOf`, `orm.IdentityOf`,
  `extensions.DefineORMOwner`, `extensions.BindSlot` and `slots.DeclareORM`.
  Thin TextHandle, OneHandle, ManyHandle and ValueHandle values keep runtime state
  behind private non-generic callbacks, with no descriptor embedding. They become
  `orm.Extension` loaders for the existing With/Load/LoadMissing methods.
  Concrete owner/key/value types, pinned storage identity, loaded-empty semantics,
  shared budgets, batching and deletion-observer cleanup remain intact.
- Model binding uses `orm.Keyed` with existing `modelbinding.ByKey`, or the new
  `ByColumn`, `ThroughRelation` and `ThroughSelectedRelation` functions. Alternate
  keys retain duplicate detection, parent scopes, retrieval hooks and eager loads;
  nested binding never falls back to a global lookup.
- `factory.Define` accepts a table and typed assignment builder. Its batch path
  uses `orm.InsertEach`: one transaction/savepoint, ordinary per-model lifecycle,
  exact returning cardinality and typed unknown-commit candidates. It adds no
  UpdateEach/DeleteEach functionality. Generated factory adapters remain usable.
- HTTP pagination now names ORM page/cursor types. Cursor types are aliases of
  the existing wire types, preserving reflected contract identities.

Capability bridges use validated `internal/sqlowner` Metadata/Query families.
Zero capabilities fail closed; capabilities never reach application callbacks.
The extension bridge accepts a concrete framework descriptor and validates the
slot's concrete value type before loading. Unique lookups reject nil, embedded
and promoted foreign field wrappers before dispatch.

## Surface and catalogue

No Query/Table methods were added. ORM snapshot additions comprise ten functions:
IdentityOf, LegacyIdentityScope, SlotOf, FoundrySlot, Extension, Keyed, Unique,
RelatedUnique, InsertEach and ParseCursor; plus ModelIdentity, Slot,
ExtensionRelation, KeyLookup, RelatedLookup, DirectRelation and four cursor
aliases. ModelIdentity exposes Validate, ModelName, Reference, ReferenceOf, Parse,
KeyToken, Scope, ActiveKeys, RetainedKeys and LockActive; both lookup handles expose
Validate/Find and redacting Format/String/GoString methods. `MaxPageSize` is a constant alias. The JSON records exact snapshot
lines, including inherited alias methods and fields.

Other additions: translations.TextOf/TextHandle, attachments.OneOf/OneHandle and
ManyOf/ManyHandle, metadata.ValueOf/ValueHandle, extensions DefineORMOwner/Scope/BindSlot/ORMIdentity,
slots.DeclareORM, modelbinding ByColumn/ThroughRelation/ThroughSelectedRelation,
factory.Define, and capability-only query FoundryExtension/FoundryPredicate.

Fourteen existing catalogue rows now exercise ORM consumers:
extension_identity_owner, extension_policy_kind, extension_rule_field,
extension_runtime_type, extension_slot_owner, extension_slot_query,
extension_value_type, extension_write_owner, factory_draft_owner,
factory_sequence_type, factory_state_owner, http_model_binding_key,
http_model_binding_locked and nested_path_value. Other rows were left alone.

## Footprint

| New probe | Functions | Ceiling |
| --- | ---: | ---: |
| Extension handles only | 1,481 | 1,704 |
| Full extension declaration | 30,790 | 35,000 |
| Bound extension namespace | 2,154 | 2,478 |
| Matched old generated namespace | 30,493 | 35,067 |
| ORM factory | 1,093 | 1,257 |
| ORM binding | 1,101 | 1,253 |

New ceilings use `ceil(measured × 1.15)`; no existing ceiling increased.
All 87 probes pass. Against accepted slice 2, q_with remains 1,211/1,211 and
aggregate_families remains 4,362/4,369. These are archive function counts, not
runtime measurements. The matched four-slot LoadMissing workload (int64 owner,
translated text, single attachment, multiple attachments, string metadata, identical
policies and runtime managers) measures 30,493 functions through the generated API
and 2,154 through the thin handles: 92.94% fewer. The declaration packages measure
35,392 old versus 30,790 new. Adapter construction still instantiates legacy manager
operations once in the declaration package, but the bound namespace no longer
exposes their generic graph to importers. A comparative guard requires the new
namespace to stay below one quarter of the old. Lookup redaction adds 12 functions
to the binding probe, within its unchanged ceiling. Only the bound namespace's existing
ceiling was lowered, from 32,865 to 2,478; other existing ceilings are unchanged.


## Acceptance

Requested cache, GOMAXPROCS=4, GOFLAGS=-p=4 and GOWORK=off were used.

- PASS: root build; database/internal and affected-package vet.
- PASS, segmented: ORM/query/querycore/ormcore, extension, pagination,
  modelbinding, factory, attachment, translation and metadata races. Scoped
  sensitive-value regressions cover both lookup handles and pointers under
  %v, %+v and %#v, plus direct String/GoString. The cursor-alias surface guard allows
  wire aliases while still walking their reachable types; a regression
  verifies that aliases cannot hide an engine. Identity reads compare old/new
  SQL and exact transaction/data statement counts, including locked `LIMIT 1`.
- PASS: fixture vet/test, fourteen compiler cases, all footprint probes,
  factory lifecycle/savepoint parity and scoped/nested binding parity.
- PASS: Makefile generation/freshness (new matched-baseline fixture only),
  docs-check, catalogue and gofmt. Prior TypeScript runtime/React/Vue and nine
  byte-identical contract-export results are retained: this review changes no
  transport/contract sources or consumer generated outputs.
- PostgreSQL smoke sources compile/vet only; no live PostgreSQL or full
  `make verify` run is claimed. Attachment races excluded PostgreSQL tests
  as required by the lane's no-PostgreSQL rule.

The fixture module gained only four indirect entries already required by the
framework's existing attachment dependencies; no version upgrades or new
framework dependencies. Source hashes, command logs and resolved initial failures
are recorded in the accompanying JSON.

## Private-field redaction follow-up

`KeyLookup` now retains its query and field state through a private pointer;
`RelatedLookup` retains only private pointers and owner type markers. This keeps
predicate values opaque when `fmt` bypasses methods on unexported wrapper fields.
Related reads copy the retained state before adding the current parent predicate.
Regressions cover private and nested wrappers, pointers, `%v`, `%+v`, `%#v`,
`fmt.Errorf` with a wrapped cause, zero handles, and repeated reads through copied
lookups with distinct parent keys and exact statement/binding checks.

Audited the other slice handles: extension slots, relations, owners and thin
manager handles already retain opaque pointers; `ModelIdentity` retains metadata
pointers and codec callbacks, and factory/model-binding adapters retain callbacks.
None retains predicate values inline. No additional API/surface or catalogue
changes; footprint sources and ceilings remain unchanged. This follow-up changes
only database/orm/lookup.go, database/orm/integration_adapters_test.go, and this
slice's report/evidence. No schema, codec, generated or transport contract changes.

Follow-up acceptance passed: root build; database/internal vet; ORM, query,
querycore, ormcore and modelbinding races; fixture vet and tests; PostgreSQL smoke
compile only; catalogue; docs-check; gofmt and diff whitespace checks. All 87
probes retain exactly their prior function counts and ceilings: binding 1,101,
extension slots 2,154, old baseline 30,493, q_with 1,211 and aggregate_families
4,362. The binding archive is 4,597,982 bytes (previously 4,590,546); no function
count growth or budget change. No PostgreSQL execution or commit.

## Shared files

Existing shared files touched by this slice: database/orm README.md,
declaration.go, relation.go, relation_tree.go, method_surface_test.go,
surface_test.go and testdata/public-methods.txt; database/query/relation_extension.go;
extensions/owner.go; http/modelbinding/key.go; nine http/pagination source files;
testkit/factory/factory.go and composition.go; tests/fixtures/goway
compiler_cases_test.go, footprint_test.go and go.mod;
blueprint/goway-data-layer/compilefail-mapping.tsv (only the fourteen rows above).
Review fixes also add direct handle/callback regression files under attachments,
translations and metadata, a matched generated baseline under
tests/fixtures/goway/oldmodels/extensions (including its ownership manifest), the
extension_slots_old probe and extension_handles_test.go. The evidence JSON lists
every new and changed slice source separately.

## Docs notes

Lead-owned docs were not edited. Record declaration-based extension owner/slot
and cleanup adapters, HTTP pagination/model binding and factory support, with
unchanged contracts and the measured 30,493-to-2,154 namespace footprint reduction.
Lookup diagnostics now redact scoped values even in private/nested wrapper fields
and wrapped-error formatting; typed manager operations retain
unknown outcomes. The declaration-only construction cost remains recorded. Slice 3b still
owns webhook/notification/attachment store migration after ordered UUID seeks
and bounded writes land. Audit/translation source deletion still needs 6e3;
datatables remains a dedicated slice. Generator retirement is not complete.
