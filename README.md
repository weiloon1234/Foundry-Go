# Foundry-Go

A strongly typed Go backend framework with Laravel-inspired developer experience.
Applications own domain models, request and response DTOs, business behavior and
explicit registrations. Foundry-Go provides infrastructure, generation and a shared
lifecycle for HTTP, CLI, workers, scheduling and WebSockets.

Use it as a Go module dependency in your team's own boilerplate. The
[team readiness review](docs/guides/final-readiness-20260927.md) accepted the
supported framework scope for that use and found no remaining implementation TODO
blocking adoption. This repository contains the framework and executable consumer
fixtures; your application skeleton belongs in its own repository.

## Start a project

Use the Go toolchain required by [go.mod](go.mod). Choose a reviewed framework
version available from your repository or module proxy, then pin its CLI and runtime
together in the application's module:

```sh
go mod init example.com/team/service
# Set FOUNDRY_VERSION to an available, reviewed tag or commit first.
go get -tool "github.com/weiloon1234/Foundry-Go/cmd/foundry@${FOUNDRY_VERSION:?select an available framework version}"
go tool foundry doctor --dir .
```

Follow [team adoption](docs/guides/team-adoption.md) for package scaffolding,
generation, migrations, private-module access and application CI. Start with
[application bootstrap](docs/guides/application-bootstrap.md), then build a real
endpoint using the [typed API workflow](docs/guides/typed-api-workflow.md).
The [configured consumer](tests/fixtures/consumer/configuredprofile/cmd/profile/main.go)
is an executable assembly example.

`application.New(settings)` assembles configured services and routes through
ordinary Go constructors. Constructors receive `application.Services` and retain
the concrete dependencies they need. Request handlers receive `context.Context`,
typed input and, where applicable, a concrete authenticated actor. Services do not
need to be forwarded through every call. Direct foundation providers remain
available for advanced composition.

## Included capabilities

| Area | Delivered functionality |
| --- | --- |
| Configuration and time | [Generated typed configuration](docs/guides/generated-configuration.md), defaults/TOML/environment/typed overrides, [named services](docs/guides/named-services.md), and [application timezone](docs/guides/application-timezone.md) shared by calendar helpers, schedules, logs and report presentation. |
| HTTP | [Typed endpoints](docs/guides/http-endpoints.md), request/response DTOs, [middleware](docs/guides/http-middleware.md), route groups, model binding, forms, multipart uploads, downloads, streams, static assets, compression and ETags. [Pagination](docs/guides/http-pagination.md) includes authenticated numbered, simple and cursor endpoints. |
| Validation and translation | [Built-in typed rules](docs/guides/validation-expanded.md) for presence, strings, formats, numbers, comparisons, collections, passwords and files; custom rules, database existence/uniqueness and bounded concurrent checks using explicitly selected connections. [Localized validation messages](docs/guides/validation-messages.md), field labels, pluralization and fallback share the catalog system. |
| PostgreSQL | [Generated models](docs/guides/model-generation.md), typed queries and writes, relations, projections, joins, aggregates, CTEs, windows, JSON/binary values, transactions, locks, hooks and soft deletes. [Named connections and read routing](docs/guides/database-routing.md); explicit [migrations and seeding](docs/guides/migrations-and-seeding.md). |
| Authentication and security | [Model-specific guards and policies](docs/guides/authentication.md), sessions, scoped tokens, password hashing, lockout, account recovery and MFA; CSRF, CORS, signed URLs, [encryption](docs/guides/encryption.md) and [webhook verification](docs/guides/webhooks.md). |
| Caching and coordination | [Typed caches](docs/guides/caching.md), tags and namespace invalidation; [Redis data and commands](docs/guides/redis-commands.md), distributed leases, rate limits and pub/sub. |
| Background work | [Jobs and workers](docs/guides/jobs.md), [failure logs and retry commands](docs/guides/jobs-operations.md), events, [scheduling](docs/guides/scheduler.md), [transactional outbox](docs/guides/outbox.md) and [HTTP idempotency](docs/guides/idempotent-operations.md). |
| Storage and communication | [Local, S3 and R2 storage](docs/guides/storage.md), attachments and imaging; [email](docs/guides/email.md), queued delivery and [notifications](docs/guides/notifications.md) with database inboxes and private realtime delivery. |
| Realtime | [Typed WebSocket channels](docs/guides/websocket.md), authorization and presence; [Redis-backed distributed fan-out](docs/guides/websocket-distributed.md) and bounded replay. |
| Application support | [Localization](docs/guides/localization.md), [settings](docs/guides/settings.md), countries, model extensions, [datatables and CSV/XLSX reporting](docs/guides/datatable.md), and named [outbound HTTP clients](docs/guides/http-client.md). |
| Operations | [Structured logging](docs/guides/logging.md), typed audit records, health/readiness checks, [observability](docs/guides/observability.md), diagnostics and lifecycle-managed shutdown. |
| Extensibility and tooling | [Typed plugins](docs/guides/plugins.md), CLI scaffolds, generation, inspection, doctor and [testing helpers](docs/guides/developer-tooling-and-testing.md). [Agent/editor tooling](docs/guides/agent-language-tooling.md) inspects actual consumer APIs with gopls. |

Configuration is explicit: the framework reads the environment lookup supplied by
the application and does not automatically load `.env` files. Go declarations
provide types, defaults and generated keys; deployment values are decoded and
validated at startup. Compilation cannot prove that a deployment supplied a
required setting. The application timezone defaults to UTC.

Logging defaults to INFO-level JSON on stderr. File channels provide automatic
daily/20 MiB rotation and retain at most 14 archives for at most 14 days, plus the
active file. Both retention limits apply; cleanup runs at startup, rollover and
hourly on a subsequent write. These policies are configurable.

## One source for API contracts and clients

The application's registered endpoint and DTO descriptors drive runtime codecs,
validation metadata, a shared manifest, OpenAPI 3.1.1 and
[generated TypeScript clients](docs/guides/client-contracts.md). The generated SDK
includes typed HTTP and realtime operations, route method/path/name metadata,
request/response fields, enum cases, access requirements, declared errors and
supported client validation rules.

The output has no runtime npm dependencies or React/Vue coupling. Frontend teams
supply transport and state management, and can compile the TypeScript to ordinary
JavaScript. Server-only validation rules are reported as skipped by client
validation; the backend remains authoritative.

Persistence models are not automatically exposed as DTOs. Declare public fields
explicitly, or use an explicit
[public query projection](docs/guides/model-projections.md) with
`//foundry:projection dto=true`. A database password column therefore does not
become an API or SDK field merely because it exists on a model. Applications still
own which data they explicitly place in public DTOs.

## Supported scope and unfinished extensions

The backend supports **PostgreSQL** and explicit **standalone Redis** connections.
Local-storage and file-cache adapters require macOS or Linux. Application boot
does not apply migrations; schema changes are an explicit operator workflow.

The following are not implemented:

- MySQL/SQLite adapters, Redis Cluster/Sentinel routing, Dart client generation,
  durable WebSocket recovery, binary WebSocket messages and Pusher/Echo compatibility.
  Bounded realtime replay is already available, with different guarantees from
  durable recovery. See [deferred extensions](blueprint/25-deferred-extensions.md).
- A general route/group documentation API for OpenAPI summaries, descriptions,
  tags, examples and custom-header explanations. Existing exports already describe
  registered types, parameters, responses, authentication and idempotency.
- Rich form-control hints such as widgets and display precision, a typed form
  controller, and automatic React/Vue form rendering. Existing SDK field types,
  enums, route metadata and validation are the foundation for this future work.

These extensions do not block a team boilerplate using the delivered scope.
Production configuration, deployment and application-specific integration/load
testing remain the consuming team's responsibility. Live real-account email
smoke sends remain unverified; local SMTP/TLS and provider contract fixtures are
covered. Native acceptance ran on macOS; Linux amd64/arm64 cross-builds passed,
which is compilation evidence rather than native Linux runtime certification.

## Verification and maintenance

The [September 27 readiness review](docs/guides/final-readiness-20260927.md)
records the framework audit, full verification, race/fuzz coverage and independent
packaged consumers. The later
[authenticated pagination review](docs/guides/authenticated-pagination-20260928.md)
records another full verification pass and affected races, compiler/editor checks
and real generated-client requests on September 28. These are dated evidence
records for their reviewed source, not guarantees for arbitrary application code
or future revisions.

For framework development:

```sh
make verify      # Formatting, vet, tests, consumers, generation, docs and release tools
make race        # Framework, plugin and consumer race checks
make docs-check  # Documentation links and repository consistency
```

Follow [contributing](docs/guides/contributing.md) for the required tools and
integration services. Consumer projects should use the
[application CI checks](docs/guides/team-adoption.md) in their own modules.

See [all guides](docs/guides), [compatibility](docs/compatibility.md),
[release checks](docs/release-checklist.md), [production operations](docs/guides/production-operations.md),
[security reporting](SECURITY.md) and the [changelog](CHANGELOG.md).
Design history and deferred scope live in the [blueprint index](blueprint/README.md);
planned blueprint examples are not shipped APIs.
