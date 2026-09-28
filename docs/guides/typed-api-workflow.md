# A complete typed API workflow

T07 passed native full verification, independent packaged consumers, PostgreSQL
races, strict generated clients, compiler/editor acceptance and bounded fuzzing.
The complete changed-code [audit](typed-api-audit.md) found and verified five
fixes/improvements. See the [acceptance record](../evidence/typed-api-t07.json).

The [team/project consumer](../../tests/fixtures/consumer/teamworkflow/application.go)
combines the delivered public contracts in ordinary Go. It is a framework fixture,
not a starter application. Its default `main` PostgreSQL connection owns teams,
projects, submissions, idempotent outcomes and the existing outbox. The explicitly
named `receiver` connection owns independent delivery receipts and receiving effects.
Constructors receive these concrete handles during assembly and keep using them
after application assembly seals. No request-context service registry is needed.

## Typed request to committed result

The authenticated project route is `/teams/{team}/projects/{project}`. The explicit
team selector and project slug select a project through the team's declared
relation. Identical slugs under another team cannot substitute for that resource.
The required guard supplies a concrete actor, and the resource policy checks the
current tenant and enabled state after binding. The fixture reuses the shared loopback
credential adapter; it is not a production identity provider.

`Patch` uses the existing `Optional[Nullable[string]]` and `Optional[int64]` types.
Preparation trims supplied text while preserving all three presence states. Rules
validate the prepared concrete DTO. The domain handler explicitly maps:

| Input | Generated mutation |
| --- | --- |
| Title omitted | No title assignment |
| Title null | `ProjectDraft.ClearTitle()` |
| Title text, including empty text | `ProjectDraft.SetTitle(text)` |
| Budget omitted | No budget assignment |
| Budget zero or a positive value | `ProjectDraft.SetBudget(value)` |

An empty patch returns the current view without inventing an empty UPDATE. Actual
updates use the injected pool's real transaction. Responses contain an explicit
`ProjectView`; stored models do not become wire DTOs implicitly.

The shared `genericdto.Envelope[Action]` composes an independently declared generic
DTO with the local tagged `Action` union. An update returns the `updated` variant
at status 200. A submission returns the `queued` variant at status 202. Generated
codecs, actual handler signatures, manifest/OpenAPI and TypeScript share these
same declarations. The public catalogue separately demonstrates the existing typed
numbered pagination and exposes only deliberately public slugs. Authenticated
project detail includes the private application view.

## Durable submission and receiving effects

`/teams/{team}/projects/{project}/submissions` applies current authentication,
request preparation/validation and resource policy before every claim or replay.
The typed callback receives the runner's `*database.Tx`, inserts a submission and
enqueues the existing `Submitted` event. Idempotency stores the complete typed
response in that same transaction. A business rejection rolls all three back.
A repeated retained scoped key with the same prepared input returns the original
representation; another app process can perform that replay.

The existing managed publisher delivers its stable message ID to `DeliverySink`.
The sink's named database transaction inserts a unique receipt and updates the
receiving counter only when that receipt is new. Two deliveries of that ID can
therefore commit only one local counter increment. Receipt retention must cover
the publisher's retry/recovery window. This is a local database guarantee; an
external provider requires its own supported idempotency protocol.

## Shared rules and generated clients

Side-effect-free public probes reuse one `NameRules` value across JSON, query,
URL-encoded form and multipart descriptors. Source-specific decoders still own
cardinality, types and error paths. Form text `null` remains the text `null`.
Generated clients preserve optional/null/value PATCH semantics, wide integers as
lossless strings, model-owned identities, discriminated result payloads and a
required branded idempotency key. Validation in the application remains authoritative
for callers that bypass the generated client.

The native TypeScript fixture sends each presence state through the actual HTTP
kernel and verifies the database afterward. It also checks wrong-parent rejection,
current resource authorization, envelopes, pagination, all input sources and replay.
The Go tests additionally cover concurrent independent applications, isolated named
pools, real receiving transactions, ordinary shutdown retaining data, and replay
from an owned native subprocess. Test schemas are retained; no database resets or
pruning are part of acceptance.

## Verification and audit status

The complete source/test/docs batch preceded compilation. The initial full gate,
focused PostgreSQL/race/client checks, independent packaged consumers and native
build/generation/editor/runtime measurements passed. The complete changed-code
[audit](typed-api-audit.md) recorded five findings and grouped their fixes.
All eight fuzz targets and the final full gate passed. Results are recorded in the
[master](../../blueprint/00-master-architecture-and-parity.md#typed-api-delivery).

## Measured local cost

Three native packaged-consumer samples on an Apple M4 Max measured medians of
1.953 ms for a new submission and 0.964 ms for replay. They include the real HTTP
client/server, nested binding and policy, PostgreSQL, typed response processing,
and managed publisher/receiver activity. Median process-wide allocations were
436,624 bytes / 6,608 allocations for new work and 173,992 bytes / 2,553 allocations
for replay; asynchronous publisher activity contributes to those totals. The
response was 160 bytes. These are local measurements, not deployment latency claims.

| Native private candidate profile | Ordinary model consumer | Integrated workflow |
| --- | ---: | ---: |
| Generated files / bytes | 1 / 68,111 | 20 / 216,143 |
| Imported packages | 174 | 545 |
| Generation: cold / unchanged / edited | 4.97 / 0.24 / 1.41 s | 22.01 / 0.47 / 4.67 s |
| Build: cold / unchanged / edited | 3.27 / 0.23 / 1.17 s | 21.52 / 0.24 / 4.48 s |
| Median completion request | 288 ms | 614 ms |
| Median hover request | 219 ms | 609 ms |

Each build/generation state has one sequential sample with its own initially
empty compiler cache. Editor figures use three fresh-server samples and exclude
initialization; command timings include process overhead. Dependency modules were
already available, with private candidate modules fetched from a verified local
proxy and no replacement directives. The workflow count includes its generic DTO
fixture dependency. The two profiles enable different features and are not an
overhead comparison. Peak sampled process-tree memory for the workflow cold build
was 7.25 GB, versus 0.51 GB for the ordinary profile; these are concurrent compiler
process totals, not request memory. No compile/test workload ran concurrently with
controlled measurements. Earlier milestone records retain the generic/union,
setup/checkout and contended-operation baselines.
