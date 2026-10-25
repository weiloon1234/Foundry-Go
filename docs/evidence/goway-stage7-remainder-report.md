# Stage 7 additive remainder

Re-applied on `f3cfffd` after batch 6b. No commit or Git writes. The corrected
[inventory](goway-stage7-remainder-plan.md) recognizes 3a's pagination, binding,
factory, identity and extension adapters and lane B's integrated declaration roles.

## API and coverage

The only framework API addition is `application.WithTables(...orm.CheckedTable)`.
It snapshots the explicit list and reuses `orm.Check`;
Build reports aggregated declaration errors before constructing providers/handlers.
Regressions cover invalid declarations, thin handles, list ownership, independent
applications and nil/zero/promoted-nil inputs. The shared Check owner now isolates
each declaration separately, retaining earlier and later errors when a promoted
nil declaration panics. Panic/Goexit failures remain classified and redacted by
`internal/callback`. The redundant whole-batch wrapper in WithTables is removed.
Direct ORM and application regressions cover the mixed invalid/promoted-nil batch.
No Query/Table methods or capability
bridges are added. The ORM surface snapshot has zero changes.

Consumers reuse 3a's cursor aliases/parser and HTTP pagination unchanged. All nine
HTTP edits and the duplicate cursor API/surface additions were dropped. The response
DTO and ownership manifest were regenerated through the Makefile. Existing generated
outputs are unchanged. The ORM seeder uses the supplied transaction and normal write
pipeline. Golden coverage compares old/new seeder SQL and cursor SQL, bindings,
mapped rows and tokens. PostgreSQL sources cover seeding/defaults and page/cursor reads.

Fourteen pagination catalogue rows are ported, with fifteen new compiler negatives
including `application_table_declaration` (720 → 735 cases). 3a's catalogue rows remain intact.
The fixture module records 24 indirect dependencies already pinned in the root
module, needed by application wiring; no new framework dependencies or versions.

## Footprint

The old combined package measured **11,483 functions / 13,206 ceiling** before
batch 6b. It is replaced by independent package-archive probes:

| Probe | Functions | Ceiling |
| --- | ---: | ---: |
| Registration (end-to-end) | 6,195 | 8,693 (unchanged) |
| Registration option only | 1,012 | 1,164 |
| Application construction only | 5,524 | 6,353 |
| Pagination | 5,306 | 6,079 |
| Seeder | 867 | 998 |

The model declaration package converts `Users.Table`, `Orders.Table` and
`WriteRecords.Table` into `CheckedTable` values and returns a fresh slice from
`models.Tables()`. Bootstrap references that interface slice. Its ownership has
an independent consumer regression.

Same-base controlled measurements include the shared Check fix on both sides:

| Workload | Direct thin handles | Model-owned interface slice |
| --- | ---: | ---: |
| End-to-end registration | 7,557 | 6,195 |
| Option only | 2,442 | 1,012 |
| Application-only control | 5,524 | 5,524 |

Five alternating before/after measurements produced identical counts for each
variant (range equals the reported value). All compilations succeeded. Only the
two caller expressions change between variants, using Go overlays. These count
T/t symbols in package archives with the same toolchain, base and r4 cache; they
are not latency or runtime measurements. The return values remain in exported
consumer functions, so their generic reachability is compiled. The interface
boundary keeps model table namespaces out of bootstrap's reachable type graph.
End-to-end registration adds 2,033 functions above the application-only control
before the change and 671 after it; that difference is an archive delta, not an
additive estimate of runtime cost.

After the shared-owner fix and removal of the whole-batch wrapper, the direct-handle
baseline is 7,557 (previously 7,559). The same-base
boundary reduction is 1,362 functions; the option-only reduction is 1,430.
The boundary is retained because both measured workloads improve. The model
declaration archive grows by two functions for the helper (6,849 → 6,851), within
its unchanged 7,070 ceiling. At review 16, every other previously measured probe count was unchanged. Review 19
adds 20 functions to eight JSON-using archives, including pagination (5,286 → 5,306),
within existing ceilings. q_with remains 1,211/1,211 and aggregate_families 4,366/4,369.

The two new control ceilings are exactly ceil(measured × 1.15). All 109 base
ceilings and the three previously recorded remainder ceilings remain unchanged.
The historical combined 11,483 count is not directly comparable to a split sum,
which repeats generic instantiations across separate importing packages.

## Acceptance

All review 21 acceptance commands passed. Current results and 76 source hashes are
recorded in [the evidence JSON](goway-stage7-remainder.json). All commands used r6
with GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off. PostgreSQL sources compile/vet only;
live PostgreSQL remains the lead's checkpoint. Only finding 6 was authorized for
this correction; H1 remains deferred.

| Command / scope | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./database/... ./internal/... ./application/... ./http/pagination/... ./enum/... ./value/... ./http/...` | PASS |
| ORM/query/querycore/ormcore/application/pagination/seed/enum/codec/database/value/safeerror/postgres/http/errorgraph `go test -race` | PASS |
| Go-way fixture `go vet ./...` and `go test -v ./...` | PASS: 735 compiler cases, 114 footprint probes, parity tests |
| Fixture `go test -race ./integrations/... ./models` and database seeder protocol race | PASS |
| Legacy pagination/client-contract consumer races | PASS |
| Fresh before/current manifest, OpenAPI and TypeScript exports | PASS: byte-identical |
| `make generate-check` | PASS |
| `python3 tools/goway_catalogue.py` | PASS |
| Repository gofmt check, `make docs-check`, `git diff --check` | PASS |

This review has segmented acceptance logs and controlled measurement records. This is the requested lane gate, not a full `make verify`
or live database acceptance claim.

## Shared files

Existing shared files changed: `database/orm/declaration.go`, the catalogue mapping (only the fourteen pagination
rows), fixture compiler-case registry, footprint registry and fixture go.mod.
Guide files: `docs/guides/application-bootstrap.md`,
`docs/guides/http-pagination.md`, `docs/guides/http-cursor-pagination.md` and
`docs/guides/migrations-and-seeding.md`.
New files are application validation/tests, the consumer packages and generated DTO,
SQL parity and PostgreSQL test sources, fifteen negatives and slice evidence.
Review additions include `database/orm/check_test.go`, the model-owned Tables helper
and ownership regression, and option-only/application-only footprint consumers.
No ORM surface snapshot, HTTP pagination implementation, lane B role declarations,
lane D stores, lane C datatables or lead-owned docs are changed.

## Docs notes

Record D7 explicit application declaration validation and the ORM seeder/pagination
consumer recipes, with fifteen new compiler negatives and fourteen catalogue ports.
3a already owns the cursor transport APIs and HTTP migration; do not credit them
again to this slice. Seed runtime semantics, schema and existing transport contracts
remain unchanged. Application validation uses a model-owned interface list and has no global
registry. Check contains failures per declaration and aggregates all diagnostics. Stage 7 is still open for other owned integrations.

## Review 17: enum diagnostics and column aggregation

The shared enum JSON boundary wraps marshaler failures with a safe message and
retains the cause for errors.Is/As, including Definition's serialization after
cached validation. EnumCodec preserves that cause in a classified fault without
copying its message. Startup regression coverage checks sentinel identity, concrete
error types and redaction through WithTables.

The declaration validator isolates every deferred column check, retaining core
errors and continuing in declaration order. A mixed table regression includes a
missing field, invalid enums before and after panicking/Goexit-ing descriptors,
and repeated Check calls against the cached aggregate.

Additional shared files: `enum/descriptor.go`, `internal/ormcore/codec.go` and
`database/orm/declaration.go`. No public signatures or footprint ceilings change.
Docs notes: enum marshaler causes remain inspectable but are redacted in normal
startup diagnostics; declaration validation aggregates failures within each table
as well as across tables.

Review 17 acceptance passed on r4: root build, scoped vet, requested races plus
enum, fixture vet/test/races, seeder protocol, generation freshness, catalogue,
formatting, docs and fresh before/current contract exports. All 114 footprint
counts and ceilings are identical to review 16; q_with remains 1,211/1,211 and
aggregate_families 4,366/4,369. All 735 compiler cases pass. PostgreSQL remains
compile/vet only. No commit or public surface additions in this correction.

## Review 18: binding diagnostics

Codec.Bind now wraps encoder/validator failures with a safe framework fault,
retaining the cause without formatting it. This shared binding boundary also
protects eager parameter capture in both expression APIs. The comparison compiler
wraps binding callback failures before returning them to declaration validation or
query execution. No public API or footprint ceiling changes.

New regressions cover codec and validator failures through through-relation
predicates, aggregate filters and captured parameters, using Check, WithTables
and ordinary ORM reads. Direct codec, compiler callback and legacy query tests
check cause identity and ensure private Error/Format methods are never called.

The read-only main audit at `10030ad4eaf6ecf6caf269dfb5e970b0816f366e`
found the same leak: `database/codec/codec.go:54-56` returns encoder/validator
errors unchanged; `internal/querycore/compiler.go:212-214` returns raw comparison
binding errors; `database/query/row_expression.go:66-68` captures those errors
in parameter nodes. `database/query/compiler.go:25` aliases the shared compiler.
Port the two shared-owner changes; legacy parameter capture then inherits the fix.

Docs notes: binding errors retain their original causes for errors.Is/As while
normal query and startup diagnostics expose only safe framework messages.
Review 18 acceptance passed on r5: build, scoped vet and races (including codec),
fixture vet/test/races, seeder protocol, consumer contracts, generation freshness,
catalogue, formatting, docs and diff checks. All 735 compiler cases passed. All
114 footprint counts and ceilings match review 17 exactly. Fresh manifest, OpenAPI
and TypeScript exports are byte-identical to the HEAD-overlay exports. PostgreSQL
remains compile/vet only. No commit.

Additional shared files: `database/codec/codec.go` and
`internal/querycore/compiler.go`. New regression files:
`application/tables_binding_test.go`, `database/codec/binding_error_test.go`,
`database/query/binding_error_test.go` and
`internal/querycore/compiler_binding_test.go`.

## Review 19: classification and remaining callback boundaries

Finding 4 is corrected first: binding and comparison redaction now uses a shared,
classification-neutral internal wrapper. An infrastructure encoder failure does
not acquire fault.Invalid. Explicit Invalid causes still classify as cursor input;
encoder, decoder and validator regressions check causes and formatter suppression,
and HTTP tests distinguish 400 from 500.

Codec decoding/scanning and cursor callbacks wrap application failures before
callback or SQL formatting. Raw SQL Valuer and Scanner adapters do the same before
entering database/sql, preserving named arguments, nil value-receiver pointers,
caller slices and stream cleanup. Database and PostgreSQL adapter error inspection use bounded errorgraph
matching with the existing context, server-error and retry precedence.

JSON snapshots use the existing v2 encoder with v1 options. The shared map-key
adapter retains a failing MarshalText cause outside the standard encoder and
passes only a safe framework error into it; the cause is reattached on return.
This also covers bounded JSON transport encoding and both query APIs' JSON map-key
helpers. String-key precedence stays compatible with v1. Native structural JSON
validation keeps its Invalid classification. Map-key decode errors are redacted
before callback formatting too.

Regression sources cover raw execution and scanning, decoder/validator failures,
JSON scalar projections and relation batches through both query APIs, cursor HTTP
classification, nested/custom JSON keys, cause identity and zero private formatter
calls. No public API or ceiling changes. All 15 acceptance steps passed; the interrupted
race and remaining steps completed on r6. All 735 compiler negatives and 114
footprint probes pass; contract exports remain byte-identical.

Eight archives gain 20 functions: extension_declaration, extension_handles,
extension_slots, extension_slots_old, framework_countries, framework_outbox,
framework_pagination and json_paths. A same-source overlay reverting only
value/json_encoding.go restores extension_handles from 1,501 to 1,481. The added
symbols are 16 compiler-generated internal/sync entry/indirect wrappers and four
equality functions reached by the safe JSON transport key adapter. All ceilings
are unchanged. This measures package-archive reachability, not runtime cost.

B37 main port files by finding (including new shared helpers):

| Finding | Files |
| --- | --- |
| 4: neutral binding redaction | `database/codec/codec.go`, `internal/querycore/compiler.go`, new `internal/safeerror/error.go` |
| 1: decode/scan and cursor | `database/codec/codec.go`, **main owner** `database/query/cursor_boundary.go` (worktree owner: `internal/querycore/cursor_boundary.go`), `value/json.go` |
| 2: SQL callback adaptation and classification | `database/execution.go`, new `database/sql_callbacks.go`, `database/errors.go`, `database/postgres/errors.go` |
| 3: JSON map keys | `value/json.go`, `value/json_encoding.go`, `value/json_map.go`, new `value/json_key_encoding.go`, **main owner** `database/query/json_path.go` (worktree owner: `internal/querycore/json_path.go`) |

Docs notes: redaction preserves error causes and classifications, and runs before
standard-library or callback formatting can invoke private error methods.

The main audit is pinned to `10030ad4eaf6ecf6caf269dfb5e970b0816f366e`.
For the complete B37 set, also retain review 17’s `enum/descriptor.go` change;
`internal/ormcore/codec.go` and ORM declaration containment are Go-way-only owners
and do not exist on that main snapshot. The shared codec change covers legacy
parameter capture in `database/query/row_expression.go` without a separate patch.

Review correction premise: the first key adapter assumed MarshalText plus v1
primitive spelling covered the native key contract. The existing executable
`TestJSONKeyNativeTextAppenderAndOneWayEncoder` disproved that assumption:
`jsonshape.MapKeyCapabilities` also declares TextAppender, and the native v2
method owner gives AppendText precedence over MarshalText. The final adapter and
regressions preserve that declared family and precedence. The PostgreSQL classifier also retains the exact SafeToRetry interface target used by custom As methods.


## Review 20: restore JSON transport classification

Only finding 6 is in this correction. Findings 1–5 and the remaining-path inventory
are deferred to the separately scheduled H1 hardening track; this slice does not
claim those paths are closed.

JSON snapshot/decode causes remain available through errors.Is/As, behind an
internal diagnostic-only boundary. HTTP classification prunes that branch using
the existing bounded error-graph traversal, including validation/authentication
fallbacks. Explicit outer transport errors and joined siblings keep their prior
precedence. The shared JSON map-path compiler inherits the boundary from NewJSON.

Regression coverage checks HTTP 500 for custom decode, snapshot, map snapshot and
map-path failures whose cause is BadRequest; wrapping, sibling/outer precedence,
cause retention and a diagnostic deadline are included. No public API or contract
changes. All 15 acceptance commands passed on r6, including full HTTP races. All
735 compiler cases and 114 footprint probes pass; every footprint count and
ceiling matches review 19, and contract exports are byte-identical. The regression
fails with 400 responses under an overlay that removes the diagnostic boundary,
and passes in the final full HTTP suite. No commit.

Main B37 port files for this correction: `value/json.go`,
`internal/safeerror/error.go`, `internal/errorgraph/walk.go`, `http/errors.go`,
`http/authentication.go`, and new `http/error_classification.go`, plus regressions.
Docs notes: retained JSON diagnostic causes do not grant an HTTP classification;
applications may still explicitly wrap the JSON failure in a transport error.


## Review 21: JSON map-path validation boundary

The diagnostic-only wrapper now also sits at JSONMapKey's formerly cause-discarding
error return. This catches key decoding and canonical re-encoding failures while
leaving direct ParseJSON map-key validation classifications unchanged. Causes
remain accessible through errors.Is/As. Regressions cover HTTP 500 for both map-path
failures and HTTP 400 for the corresponding direct ParseJSON calls, plus existing
outer/sibling precedence checks. H1 remains deferred. All 15 acceptance commands
passed on r6. Both new map-path regressions fail with HTTP 400 under the pre-fix
overlay and pass in the final full HTTP race suite. The direct ParseJSON controls
remain HTTP 400. All 735 compiler cases pass; all 114 footprint counts and ceilings
are unchanged, including q_with at 1,211/1,211 and aggregate_families at 4,366/4,369.
Manifest, OpenAPI and TypeScript exports remain byte-identical. PostgreSQL was
compiled/vetted only. No commit.

Main B37 port: apply this return-boundary change in `database/query/json_path.go`
(main owner); the worktree owner is `internal/querycore/json_path.go`.
Docs notes: JSON map-path declaration failures retain diagnostic causes without
changing their transport classification; direct JSON key validation is unchanged.


## Lane A9 rebase at 6d80111

Resolved compiler/footprint conflicts by union: all 777 base compiler cases and
125 base footprint entries are unchanged, with 15 compiler cases and five probes
from the accepted remainder added. All 15 acceptance steps passed on r6. Fresh
manifest/OpenAPI/TypeScript exports are byte-identical to the new HEAD baseline.
The fixture suite passes 792 compiler cases and 130 footprint probes. No ceilings
were raised; q_with remains 1,211/1,211 and aggregate_families 4,366/4,369.

Current probe counts on this newer base: registration 4,007; option-only 1,010;
application-only 3,339; pagination 5,305; seeder 858. Historical measurements above
remain historical: these newer-base counts do not isolate a remainder performance
change. No code or files in the old laneA worktree were changed.
