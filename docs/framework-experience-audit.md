# Framework developer experience audit

Historical source audit from 2026-09-17 against the then-delivered source and Laravel 13 documentation.
Its consumer-startup continuation is now implemented and verified through C01–C05;
see the [final acceptance](guides/consumer-startup-acceptance.md). The sections below
retain the original findings and proposed follow-ups. The
[master roadmap](../blueprint/00-master-architecture-and-parity.md) owns delivery status.

## Objective and verdict

The agreed direction is **Laravel-like developer convenience, fully typed Go
contracts, IDE completion, and measured Go performance**. Foundry chooses and owns
infrastructure libraries; consumers write configuration and domain behavior.
Configurable services expose named instances and an automatically selected
default. PostgreSQL is the only database adapter in scope.

At the time of this audit, Foundry supplied substantial typed web infrastructure
but did not yet meet the complete configured startup experience. The largest gap was
assembly and default selection, rather than replacing the feature implementations.
`DefaultConfig()` initializes settings; it does not select a default service.
Constructing several instances manually is also different from a framework-owned
configuration registry.

The original milestones 01–24 remain accepted for their recorded contracts. This
review identifies the additional work needed for the clarified consumer objective.

## Evidence and verification boundary

The review followed public constructors, configuration, provider modules,
registries, independent consumers, adapter implementations and current guides.
Before documentation edits, all **3,538** paths in the last accepted source
manifest matched their SHA-256 hashes. The existing
[production acceptance](production-acceptance.md) therefore remains relevant to
that implementation; the full Go suite was not rerun for this documentation audit.

This review also searched production exported signatures for external library
types and checked the named/default selection boundaries. This is not a new
live-cloud certification or an exhaustive proof of every runtime behavior.
Proposed assembly must receive its own consumer, compiler, editor, lifecycle and
performance acceptance before it can be described as delivered.

## Requested web capabilities

| Capability | Delivered boundary | Improvement needed |
| --- | --- | --- |
| Database | PostgreSQL, typed models/query AST, relations, transactions, migrations, seeders, primary/read routing | Named logical connections, default selection and configuration-driven setup; retain PostgreSQL only |
| Cache | Typed keys/values, expiry, Remember, tags, counters; memory and Redis implementations | Named stores/default, file and PostgreSQL adapters, capability-aware configuration |
| Storage | Typed disks, object operations, local managed store, AWS S3, R2, upload/download bridge | Configured disks/default and framework-owned credential configuration |
| Image | Owned image engine, typed plans and results | Default engine setup and reusable typed transform presets; no consumer codec-library choice |
| File | Request-owned uploaded files, readers, metadata, downloads, disk file/byte conveniences | Clarify object storage versus ordinary filesystem work; a root-scoped filesystem API would be additional work |
| Request | Typed path/query/body inputs, validation, upload and browser-auth transport | Application assembly should install standard request policies; preserve typed input instead of adding dynamic property access |
| HTTP server | Owned router/server and lifecycle based on Go's standard HTTP library | Configured web assembly and a small executable consumer proving the ordinary path |
| Outbound HTTP | Owned bounded client, response/error contracts, per-upstream modules | Named clients/default, configured upstreams and one place for headers, timeouts and trace policy |
| Middleware | Framework definitions, routing composition, auth/security/rate-limit/compression features | Standard typed middleware groups with documented ordering and explicit customization |
| Route | Typed descriptors, URL generation, access policy, endpoints and generated transport contracts | Reduce repeated registration plumbing while keeping declarations as the source of truth |

Source examples: [root assembly](../foundry.go),
[foundation builder](../foundation/builder.go),
[typed endpoints](../http/endpoint.go), [router](../http/router.go),
[middleware](../http/middleware.go), [uploads](../http/uploaded_file.go),
[image engine](../imaging/engine.go), [HTTP client module](../httpclient/module.go).
Consumers already need no Gin/Echo selection or separate image library selection.
The ordinary API should continue to use Foundry types and appropriate Go standard
types such as `context.Context`, `io.Reader` and `time.Duration`.

## Named instances and automatic defaults

Apply the same selection rule across these services, while retaining their
domain-specific names and concrete types:

| Service | Named instances | Automatic default | Important distinction |
| --- | --- | --- | --- |
| Database | Connections, e.g. primary application and reporting | Default database connection | Each logical connection can itself own primary/read pools |
| Storage | Disks, e.g. documents, avatars, archives | Default disk | Two disks can use the same adapter with different roots/buckets |
| Cache | Stores, e.g. application, reports, ephemeral | Default store | A store's Redis connection or PostgreSQL connection is a separate reference |
| Redis | Connections, e.g. cache and background work | Default Redis connection | A Redis database number does not isolate Redis pub/sub |
| Mail | Mailers, e.g. transactional and bulk | Default mailer | Transport, sender and credentials belong to each mailer |
| Jobs | Backend connections | Default connection and default queue within it | A queue name is not a backend connection |
| Outbound HTTP | Clients/upstreams | Default HTTP client | Each named client owns its origin, credentials and limits |
| Logging | Channels and composed stacks | Default channel/stack | Fan-out is composition, not multiple competing global loggers |
| Pub/sub and realtime | Broker/transport connections | Default transport | Application topics, rooms and channels remain separate typed declarations |
| Authentication | Model-owned guards/providers | Default guard within a typed model/route-group binding | A runtime choice must not erase the authenticated model's Go type |

For image processing, translation, validation, encryption and request policies,
provide default configuration and typed presets where useful. Do not introduce a
connection manager merely for naming consistency. A session-data facility, if
added, should have a configured default backend; multiple application sessions
are not the same thing as multiple backend connections.

Laravel directly supports named/default
[database connections](https://laravel.com/framework/docs/13.x/database),
[storage disks](https://laravel.com/framework/docs/13.x/filesystem) and
[cache stores](https://laravel.com/framework/docs/13.x/cache). Its
[queue documentation](https://laravel.com/framework/docs/13.x/queues#connections-vs-queues)
distinguishes backend connections from queues. These are the reference semantics;
Foundry's public expression of them should remain typed Go.

### Default contract

1. Each enabled manager has one configured `Default` referencing an existing
   named instance. Framework settings provide useful defaults where no external
   credentials are needed. External credentials and infrastructure are supplied
   by the application environment.
2. An ordinary operation or ordinary constructor binding automatically uses
   that default. Selecting a different instance uses its typed declaration or
   handle. Consumers should not repeat the default's name at every call site.
3. There is no special backend named `DEFAULT` and no separate pool for the
   default alias. Default and named access resolve to the same owned instance.
4. Validate unknown defaults, missing names, duplicate names, driver/config
   mismatches and dependency cycles before opening external resources. Never
   choose the first entry of a map or silently replace a missing named instance
   with the default.
5. Defaults are immutable within an application. Per-request alternatives use
   explicit typed handles; a request must not mutate a process-global default.
   Two applications and parallel tests must remain isolated.
6. Environment/file names are parsed at the configuration boundary. Go type
   checking distinguishes service kinds and payload/model types; boot validation
   proves configured-name membership. A string-based typed ID alone cannot make
   an arbitrary environment value a compile-time checked name.

The intended consumer experience is: one configuration declaration, normal calls
automatically targeting its default, and autocompletable declarations for named
alternatives. Exact method names require API design and consumer compile checks.
Reuse the existing [typed configuration loader](../config/schema.go), including
its precedence, ownership and secret handling; do not add a second settings parser.

## Concrete gaps, ordered by impact

### 1. Application assembly is still consumer work

[foundry.New](../foundry.go) delegates to `foundation.NewBuilder`. The builder
assembles registered providers; it does not install configured database, cache,
storage, mail or HTTP services. The
[storage consumer](../tests/fixtures/consumer/storing/files.go) defines separate
backend keys, providers, dependency lists and resolver closures for local versus
cloud storage. The [cache consumer](../tests/fixtures/consumer/caching/redis.go)
manually combines the Redis module and `cache.NewStore`.

Add a framework-owned configured assembly layer over these existing modules.
Feature packages must continue not to import application assembly. Keep ordinary
domain constructor injection and the advanced provider extension boundary.
Central configuration should determine supported adapters; domain code should
not change when an adapter changes. Framework-owned migrations are contributed
to explicit tooling, never run automatically during ordinary boot.

### 2. Named/default managers are incomplete

[Storage.Registry](../storage/registry.go) already resolves declared disk IDs and
rejects duplicates, but has no configured default or adapter construction.
[PostgreSQL routing](../database/postgres/routing.go) owns a primary and optional
read endpoint for one database service, not a named-connection manager.
Multiple explicit database/Redis/mail/client modules are possible, but are not a
consistent configured consumer API. [Logging](../logging/logger.go) provides
structured JSON logging rather than a configured channel/stack manager.

Reuse existing registries and lifecycle owners. Standardize default-selection
semantics without replacing every public service with a generic `any` manager.
Cache values, model identities, request DTOs and authenticated models must retain
their existing concrete types.

For databases, resolve the connection before opening a transaction and retain
that executor through model operations, hooks and after-commit behavior. Do not
route a transaction back through the current default. Cross-connection joins and
distributed atomic commits are not implied. Migration/seeder commands and health
output need explicit/default connection selection, and aggregate pool budgets
must account for all named primary/read pools.

### 3. Multi-connection jobs require kernel composition

Each [jobs.Module](../jobs/module.go) registers the Worker kernel, and
[Registrar.Kernel](../foundation/services.go) rejects duplicate kernel kinds.
Therefore registering two ordinary job modules is not a working multi-connection
worker design. The [WebSocket module](../websocket/module.go) has a corresponding
single-kernel constraint.

Separate configured connection/dispatcher ownership from worker-kernel assembly.
One kernel registration should run the explicitly selected worker configuration;
multiple connection definitions must coexist without duplicate kernels. Keep
queue names, routing and backend selection separate. Preserve per-backend atomic
workflow and outbox guarantees; switching connections must not imply cross-backend
atomicity. A PostgreSQL outbox is not itself a PostgreSQL queue adapter.

### 4. Cache adapter coverage is narrower than requested

The concrete cache implementations are [memory](../cache/memory/memory.go) and
[Redis](../redis/cache.go). There is no file or PostgreSQL cache adapter. Add both
behind the existing contracts and configure them through named stores.
PostgreSQL covers the requested relational-cache use case without adding MySQL.

Adapter switching needs more than `Get`/`Put`: expiry, atomic add, counter bounds,
tag invalidation, fill coordination and locking have observable semantics.
Declare required capabilities and reject incompatible combinations at assembly.
Local memory/file capabilities must not silently claim distributed behavior.
File operations need process-safe concurrency, root confinement and bounded
cleanup; PostgreSQL storage needs explicit migrations and bounded expiry pruning.

### 5. Ordinary cloud configuration leaks SDK types

[storage/s3.Config](../storage/s3/config.go) exports `aws.CredentialsProvider` and
`aws.HTTPClient`; `R2Config` requires an AWS credential provider.
[email/ses.Config](../email/ses/driver.go) also requires that provider type.
The framework already chooses the AWS SDK, but the ordinary configuration path
still exposes its vocabulary to the consumer.

Provide one reusable Foundry-owned credential configuration/provider boundary for
S3, R2 and SES. Preserve credential-chain/rotation behavior and `secret.String`
handling. SDK-specific customizations may remain in an explicit advanced bridge;
ordinary framework consumers should need only Foundry and standard-library imports.

### 6. File semantics and full browser conveniences need explicit scope

The [local storage implementation](../storage/local/backend.go) is a managed
object store. Its hashed internal files include framework metadata; it is not a
directory of ordinary uploaded files that can be exposed by a public symlink.
The existing upload/storage/download bridge is useful and should remain intact.

If the objective includes Laravel-style local file manipulation, add a separate
root-scoped filesystem facility for ordinary files, directory traversal and
atomic writes. Reuse internal path/ownership primitives. Do not silently change
the managed local disk format or introduce a second storage protocol.

Existing [sessions](guides/sessions.md) and
[browser integration](guides/browser-sessions.md) concern authentication credentials,
cookies, rotation and CSRF. A general typed session-data/flash/old-input facility
was not found. Likewise, static/SPA delivery exists, but a general server-rendered
view/template facility was not found in the public HTTP boundary. These are
additional needs for a full server-rendered experience; neither blocks a typed
API plus Vue/React application. They should receive separate scoped contracts.

### 7. Consumer guidance and proof should show the normal application path

The current consumer fixture is an extensive capability catalog. It does not
prove that configuration alone assembles a small complete web application.
Add a compact independent acceptance fixture for that path, not a starter product.
Keep the existing scaffolds for models, DTOs, jobs, commands, migrations and seeders.

Several live guides still described completed work as pending. This audit
corrects the directly reviewed README/foundation/storage/session/HTTP status
statements against the master evidence. Historical milestone logs remain historical;
additional guide cleanup should avoid changing actual capability limitations.
Lead onboarding with a short runnable path and link the detailed catalog afterward.

## Other web infrastructure already present

The package inventory includes validation and typed errors; auth, authorization,
passwords, recovery and MFA; encryption and secrets; events, audit and outbox;
jobs, scheduling and leases; mail and notifications; WebSockets and pub/sub;
localization; attachments, metadata and application settings; datatables and
exports; CLI/scaffolding; health, maintenance, observations and tracing; plugins;
test helpers; generated OpenAPI and TypeScript contracts.

These should participate in the same configured lifecycle and typed consumer
experience. Avoid introducing competing libraries or duplicate schemas for them.
Search engines, payments, social login and process orchestration are separate
extensions unless the application scope requires them. Laravel's
[documentation inventory](https://laravel.com/framework/docs/13.x)
is a useful checklist, not a requirement to clone every ecosystem package.

## Proposed implementation batches

These are proposed follow-up batches, not delivered numbered milestones.

| Order | Complete implementation batch | Completion evidence |
| --- | --- | --- |
| A | Shared default/typed-name contracts and configured application assembly; default/named PostgreSQL, Redis and storage; framework-owned cloud credentials | Compact independent consumer, two applications without shared state, config-only adapter switching, invalid configuration rejected before boot |
| B | Named/default cache plus PostgreSQL and file backends | Shared backend contracts, capability checks, real PostgreSQL and file concurrency/expiry/isolation tests |
| C | Named/default mail, queues, outbound clients, logging and realtime transports; single-kernel composition; typed auth defaults | Two configured instances of each relevant service, explicit routing, unchanged model types, lifecycle/failure/isolation coverage |
| D | Normal web assembly, middleware groups and storage/image/file conveniences; onboarding and scaffolding polish | Small end-to-end upload/image/store/cache/database/HTTP workflow with framework-only infrastructure imports |
| E | Whole-change verification and independent re-audit of consumer code, ownership, errors, docs and resource use | Full required gate, fix batch, affected reruns and final-source evidence |

General session data, server-rendered views and ordinary filesystem breadth need
their own focused design before being included in a batch. API/SPA consumers
should not have to install these merely to use the core web path.

For each batch: finish implementation, test sources and documentation first;
then compile/test, collect failures, batch fixes and repeat until accepted. Do
not rebuild the full framework after each minor edit.

## Acceptance for the clarified objective

- The minimal consumer declares domain data/handlers and typed configuration.
  It has no infrastructure resolver closures for ordinary built-in services and
  no mandatory AWS, pgx, Redis, router or image-library imports.
- Default selection requires no repeated name. Every named/default pair shares
  one actual instance and lifecycle owner. Missing or mistyped external names
  fail deterministically; lookup never silently falls back.
- Two PostgreSQL connections, two disks using the same adapter, two cache stores,
  and two queue connections work together with explicit resource bounds and
  without duplicate kernels or duplicate shutdown.
- Local/S3/R2 selection changes configuration without changing domain storage
  calls. Memory/file/PostgreSQL/Redis cache selection does the same within
  validated capabilities. Credentials remain private.
- Compiler-negative fixtures reject wrong service-kind selectors, wrong cache
  values and mismatched model/guard types. Real gopls probes cover default access,
  named declarations, config fields, endpoint DTOs and generated model methods.
- Configuration parsing and driver dispatch occur during assembly. Request paths
  use resolved typed handles; no new reflection-based lookup, config decoding or
  per-call provider construction is introduced.
- Native benchmarks compare the existing and new default/named paths for request
  latency, allocations, startup, memory and shutdown. Reuse the
  [resource measurement method](guides/developer-resource-measurements.md) for
  cold/incremental builds and IDE behavior. Do not assert performance from the
  choice of Go alone.
- A fat framework need not start every service. Boot only configured/enabled
  resources. Go still compiles imported dependencies even when runtime config
  disables a feature: measure the configured assembly's import/build cost and
  preserve narrower package consumption for users who need it.
- The final review checks the complete consumer workflow and fixes what it finds.
  Existing component acceptance alone cannot close this new experience objective.
