# Stage 7 slice 3b

Base: `05b830c`, with the accepted slice 3a delta retained. No commit.
The six requested conflicts were resolved by union. The regenerated union kept
all 3,081 snapshot lines; slice 3b adds only the `rows.Each` function signature.
No Query/Table methods or compile-fail catalogue rows are added by 3b.

## Webhooks

Endpoint, Secret and Delivery now use explicit ORM declarations. Consumers retain
endpoint/secret lock order, expiration filters, delivery identity, network-effect
handling, replay and reconciliation. UUID maintenance cursors use `orm.UUIDGreater`.

## Notifications

Envelope, Delivery and Inbox now use ORM declarations and typed conflicts. Capture
still refreshes after conflict-ignore; delivery claim/fingerprint handling and the
abandoned-Running rule remain in their existing owners. Recipient predicates stay
attached to keyed writes. Inbox batches use `UpdateEach`/`DeleteEach`; pruning keeps
its bounded key subquery. Mark-read replaces its clear assignment rather than
emitting two assignments for the same field.

## Attachments

File and Variant use ORM declarations and UUID seeks. Existing bounded write
loops retain their per-row keyed writes and savepoints; they are not fused into
a different lifecycle batch. Narrow index reads retain locks and exclude payload columns. Storage
operation identity, writer fencing, retained/orphan states, publication outcomes,
cleanup ordering and reconciliation remain in the existing managers. The accepted
owner/slot handles continue to delegate to these managers.

## Extension store

Meta, Setting and Translation use ORM declarations. Narrow row schemas retain the
original index/state result types and codecs. Settings preserve conditional
presentation ownership, value/version conflicts and cache invalidation. Metadata
and translations retain pinned owner identities, scope filters and lock ordering.
The two streaming projection consumers use the new package function
`rows.Each(ctx, executor, projection, yield)` over the shared read/close path.
It adds no generic methods or embedding chains.

## Audit and translation source deletion

Audit Entry uses ORM declarations and typed append assignments. Activity reads
remain payload-free. Pruning uses `rows.DeleteUsing` with the original area/cutoff,
oldest insertion-sequence ordering and limit. Translation deletion uses DeleteUsing
after the existing owner lock, without hydrating text or adding per-row callbacks.

## Contracts, sources and generation

- Twelve model shapes, persisted names, tags, codecs and identity key types are
  retained. Current timestamp carriers are used; each public field handle has an
  example and applicable creation/update/default behavior documentation.
- Makefile generation removed 17 owned model/projection files and updated five
  ownership manifests. The final complete `make generate` succeeded.
- Nine freshly exported manifest/OpenAPI/TypeScript artifacts have identical
  before/after SHA-256 values. No transport declaration changes were introduced.
- Added 59 old-store transcript cases covering reads, inserts, updates, identities,
  UUID seeks, lifecycle batches and source deletion. They pin SQL, bindings,
  hydration and transaction/statement counts; the old transcripts are immutable.
- Added external-package streaming regressions for callback/decode errors,
  cancellation, fresh scope context, nil callbacks, panic/Goexit containment,
  row closure and a single data statement. They use only public ORM/rows APIs.

## rows.Each integration

Lane C's reviewed `database/orm/rows/read.go` owns the shared `rows.Each` API.
This lane's `each.go` is a self-contained temporary copy of that function and its
imports; drop it at integration and retain the public-API regression file.

The original lane D version propagated callback panics and checked decoder presence
only after obtaining a row. Lane C validates callback/decoder presence before I/O
and invokes callbacks with `callback.Isolated`: panic and Goexit become redacted
faults, ordinary errors retain `errors.Is` identity, and execution waits for the
callback to exit before closing rows. Lane D now uses the identical function body.

Both versions use `ormcore.Read`, preserving explicit predicates, request-time
global scopes, cancellation, complete decoding and close-error aggregation. The
store consumers require bounded streaming, first-error termination and row closure;
they do not rely on panic propagation, goroutine identity or unwrapped error text.
No remaining semantic difference is relied on by settings or translations.

## Verification status and footprint

The rebase-only 3a checkpoint completed before the 3b migration: build, vet, races,
fixture vet/test, catalogue, docs and pgsmoke compilation passed. Its interrupted
fixture segment was resumed on r3. Those historical results do not verify 3b.

The earlier decision to defer verification was incorrect and has been superseded
by the requested full acceptance. All commands use r3, GOMAXPROCS=4,
GOFLAGS=-p=4 and GOWORK=off. Current command outcomes and source hashes are recorded
in the JSON. Full slice 3b acceptance passed.

PASS: `go build ./...`; full requested database/internal and migrated-package
vet; segmented races for ORM/query/querycore/ormcore, the five store packages,
storemigration, webhook/notification/audit consumers and extension consumers;
all four fixture modules' vet and tests (pgsmoke compile only); catalogue;
`make generate`; `make generate-check`; `make typescript-check` (real
HTTP/WebSocket plus React/Vue); `make docs-check`; gofmt and `make fmt-check`;
`git diff --check`. The 59 old-store transcripts pass. Catalogue remains 465/1,026
rows ported by 457 distinct cases; 3b changes no catalogue rows.


Vet caught a leftover generated Explain call in a PostgreSQL source; it now uses
the ORM-compiled statement with unchanged bindings and a non-executing EXPLAIN.
Notifications initially hit the sandbox's loopback restriction and passed when
rerun outside that sandbox. No live PostgreSQL ran.

| New probe | Functions | Ceiling |
| --- | ---: | ---: |
| webhookstore | 4,705 | 5,411 |
| notificationstore | 5,119 | 5,887 |
| attachmentstore | 4,729 | 5,439 |
| extensionstore | 5,407 | 6,219 |
| auditstore | 4,753 | 5,466 |
| rows.Each consumer | 899 | 1,034 |

These are T/t symbol counts in non-race Go package archives, not runtime speed
measurements. New ceilings use ceil(measured × 1.15). Existing ceilings remain
unchanged: q_with is 1,211/1,211, aggregate_families 4,366/4,369. The matched
extension namespace now measures 2,093 through thin handles versus 8,530 through
the legacy generated extension API on the same migrated framework. Both workloads
benefit from retiring generated internal stores; the one-quarter relative guard
still holds. This comparison does not claim a whole-application speedup.

## Shared files

Conflict union: database/orm/README.md, method_surface_test.go, surface_test.go,
testdata/public-methods.txt; tests/fixtures/goway/compiler_cases_test.go and
footprint_test.go. Slice 3b additionally changes database/orm/README.md,
method_surface_test.go and testdata/public-methods.txt;
adds rows/each.go and each_test.go; and changes the five store packages plus their
attachments, audit, metadata, settings, translations, notifications and
webhook/outbound consumers/tests. Exact source paths and hashes are in the JSON.
The accepted 3a catalogue rows and earlier edits remain intact. Six new archive
probes are registered in footprint_test.go; rows_each/p.go exercises the shared
public streaming function. The PostgreSQL explain source also changed as above.

## Docs notes

Lead-owned docs were not edited. Record completion of all thirteen framework-store
migrations across slices 1–3b, with lane acceptance passed; extension managers now
use declared persistence. Ordered UUID seeks, bounded lifecycle batches and
source deletion are used by production framework consumers. Datatables remains
the dedicated next slice; generator retirement remains Stage 8. Lane B may migrate
these declarations to its new typed role carriers when integrated.
