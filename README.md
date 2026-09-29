# Foundry-Go

A strongly typed Go backend framework with Laravel-inspired developer experience.
Applications own domain models, request and response DTOs, business behavior and
explicit registrations. Foundry-Go provides infrastructure, generation and a shared
lifecycle for HTTP, CLI, workers, scheduling and WebSockets.

Use it as a Go module dependency in your team's own boilerplate. The
[team readiness review](docs/guides/final-readiness-20260927.md), the subsequent
[stabilization review](docs/guides/stabilization-20260929.md) and the
[second independent review](docs/guides/second-review-20260929.md) record the
supported scope, verification and required upgrade steps. This repository contains the
framework and executable consumer fixtures; your application skeleton belongs
in its own repository.

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
| HTTP | [Typed endpoints](docs/guides/http-endpoints.md), request/response DTOs, [middleware](docs/guides/http-middleware.md), route groups, model binding, forms, multipart uploads, raw bodies, downloads, streams, server-sent events, redirects, multiple success statuses, per-route timeouts and body limits, static assets, compression, ETags and typed cache headers. [Pagination](docs/guides/http-pagination.md) includes authenticated numbered, simple and cursor endpoints. |
| Validation and translation | [Built-in typed rules](docs/guides/validation-expanded.md) for presence, strings, formats, numbers, comparisons, collections, passwords and files; custom rules, database existence/uniqueness and bounded concurrent checks using explicitly selected connections. [Localized validation messages](docs/guides/validation-messages.md), field labels, pluralization and fallback share the catalog system. |
| PostgreSQL | [Generated models](docs/guides/model-generation.md), typed queries and writes, relations (including through, one-of-many and polymorphic), pivot sync, [global scopes](docs/guides/model-global-scopes.md), set-based writes, projections, joins, aggregates, CTEs, windows, JSON/binary values, transactions with retry, locks, hooks, soft deletes and [pruning](docs/guides/model-pruning.md). [Named connections, read routing and sticky reads](docs/guides/database-routing.md); explicit [migrations and seeding](docs/guides/migrations-and-seeding.md) with optional confirmed rollback. |
| Authentication and security | [Model-specific guards and policies](docs/guides/authentication.md), sessions, scoped tokens, password hashing, lockout, account recovery, MFA, impersonation and [social login](docs/guides/social-login.md); CSRF, CORS, signed URLs, encrypted cookies, [encryption key rotation](docs/guides/encryption.md) and [inbound and outbound webhooks](docs/guides/webhooks.md). |
| Caching and coordination | [Typed caches](docs/guides/caching.md), tags, namespace invalidation and stale-while-revalidate; [Redis data and commands](docs/guides/redis-commands.md), distributed leases and semaphores, [rate limits](docs/guides/rate-limiting.md) and pub/sub. |
| Background work | [Jobs and workers](docs/guides/jobs.md) with job middleware, workflows and a sync driver, [failure archive and queue commands](docs/guides/jobs-operations.md), events with queued listeners, [scheduling](docs/guides/scheduler.md), [transactional outbox](docs/guides/outbox.md) and [HTTP idempotency](docs/guides/idempotent-operations.md). |
| Storage and communication | [Local, S3, R2 and S3-compatible storage](docs/guides/storage.md), presigned uploads, attachments with image variants and imaging; [email](docs/guides/email.md) with failover transports, queued delivery and [notifications](docs/guides/notifications.md) with database inboxes, on-demand routes and private realtime delivery. |
| Realtime | [Typed WebSocket channels](docs/guides/websocket.md), authorization and presence; [Redis-backed distributed fan-out](docs/guides/websocket-distributed.md) and bounded replay. |
| Application support | [Localization](docs/guides/localization.md), [settings](docs/guides/settings.md), countries, model extensions, [datatables, CSV/XLSX reporting and imports](docs/guides/datatable.md), money and decimal rounding, and named [outbound HTTP clients](docs/guides/http-client.md). |
| Operations | [Structured logging](docs/guides/logging.md), typed audit records, public liveness/readiness probes, redacted server-failure diagnostics, [observability](docs/guides/observability.md) with Prometheus metrics and OTLP traces, fleet-wide [maintenance mode](docs/guides/readiness-and-maintenance.md), an opt-in [housekeeping schedule](docs/guides/production-operations.md) and lifecycle-managed shutdown. |
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
- Go doc comments as OpenAPI schema descriptions and custom-header explanations.
  Route documentation (summary, description, tags, deprecation) and typed
  request/response examples are exported.
- Rich form-control hints such as widgets and display precision, a typed form
  controller, and automatic React/Vue form rendering. Existing SDK field types,
  enums, route metadata and validation are the foundation for this future work.
- Token-family impersonation, a cache failover store, direct local `sendfile`,
  lossy WebP output, and test/factory/mail/observer scaffolds. Session impersonation,
  lossless WebP and the existing testing/factory APIs are available; see the
  [improvement program's remaining work](docs/guides/improvement-program-20260929.md#not-delivered).

These extensions do not block a team boilerplate using the delivered scope.
Production configuration, deployment and application-specific integration/load
testing remain the consuming team's responsibility. Live real-account email
smoke sends remain unverified; local SMTP/TLS and provider contract fixtures are
covered. Framework acceptance ran on macOS; Linux amd64/arm64 cross-builds passed.
The subsequent stabilization also verified an independently packaged starter on
Linux arm64, including integration/race tests and non-root process shutdown.
Live systemd supervision and the complete framework suite on every Linux
architecture remain outside that evidence.

## Verification and maintenance

The [September 27 readiness review](docs/guides/final-readiness-20260927.md)
records the framework audit, full verification, race/fuzz coverage and independent
packaged consumers. The later
[authenticated pagination review](docs/guides/authenticated-pagination-20260928.md)
records another full verification pass and affected races, compiler/editor checks
and real generated-client requests on September 28. The
[improvement program record](docs/guides/improvement-program-20260929.md) covers
the September 29 audit fixes and parity work with a fresh `make verify`, TypeScript,
editor and full race pass. The subsequent
[stabilization review](docs/guides/stabilization-20260929.md) records additional
auth, retry and cancellation fixes, final `make verify`, affected race suites,
independent packaged consumers and repeated starter queue-process tests on macOS
and Linux arm64. The [second independent review](docs/guides/second-review-20260929.md)
then fixed further defects across every changed area and passed a fresh
`make verify`, TypeScript, editor and full race pass on its revision. The later
[acceptance follow-up](docs/guides/second-review-acceptance-20260929.md) passed
PostgreSQL races, security, fuzzing, packaged consumers and ordinary starter
verification on macOS/Linux; an additional Linux queue stress timeout still
prevents complete release acceptance. These are dated evidence
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
