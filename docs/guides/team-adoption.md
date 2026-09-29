# Using Foundry-Go in a team project

Keep your boilerplate in its own Go module. Import Foundry-Go for infrastructure;
own your application settings, domain models, DTOs, handlers, policies and explicit
registrations. Start with `application.New(settings)` and the existing
[configured consumer](consumer-startup-acceptance.md), then follow the
[complete typed API workflow](typed-api-workflow.md) for authenticated database work.

The [September 27 readiness review](final-readiness-20260927.md) accepts the current
framework for team boilerplate development, with full verification, races and
independent package consumption. It records supported scope and distribution steps.

Set `application.Settings.TimeZone` for the team's calendar defaults (UTC when
omitted). Date helpers, schedules, logs and report presentation share it; see
[application timezones](application-timezone.md) for constructor injection and overrides.

## Pin the runtime and development tool together

Use the Go requirement in the selected framework's [go.mod](../../go.mod).
Choose a version your team has made available through its repository or module
proxy. A local candidate version in an acceptance report is not a published tag.
The framework owner must make the reviewed source available before ordinary
remote installation can work; see [release checks](../release-checklist.md).

From a new project directory:

```sh
go mod init example.com/team/service
# Set FOUNDRY_VERSION to the team's available, reviewed version first.
go get -tool "github.com/weiloon1234/Foundry-Go/cmd/foundry@${FOUNDRY_VERSION:?select an available framework version}"
go tool foundry doctor --dir .
```

The `tool` directive and ordinary application imports use the same Foundry-Go
requirement in `go.mod`. `go tool foundry` therefore follows the project's module
graph when the framework is upgraded; it needs no separately installed global
CLI. `foundry generate` and `foundry doctor` compare the tool's framework release
with the module's selection and fail on a known mismatch, so an older global
binary cannot silently generate code for a newer runtime. Retain `go.mod` and `go.sum` in the boilerplate. The
[independent consumer](../../tests/fixtures/consumer/go.mod) uses this arrangement,
and its [tool acceptance](../../tests/fixtures/consumer/pinned_tool_test.go)
checks the real CLI and generated model metadata.

For private hosting, configure Go's private-module access for the actual module
path and provision repository credentials outside source control. A local
`replace` is useful during framework development; remove it before testing the
team's dependency distribution. Verify the selected version with:

```sh
go list -m github.com/weiloon1234/Foundry-Go
go mod verify
```

## Build your boilerplate from the public contracts

| Application concern | Starting point |
| --- | --- |
| Settings, environment/TOML and startup | [Application bootstrap](application-bootstrap.md), [generated configuration](generated-configuration.md), and the executable [configured profile](../../tests/fixtures/consumer/configuredprofile/cmd/profile/main.go) |
| Domain models and schema evolution | [Typed API workflow](typed-api-workflow.md), [migrations and seeding](migrations-and-seeding.md) |
| Typed routes, validation and responses | [Endpoints](http-endpoints.md), [DTOs](http-dtos.md), [forms and request lifecycle](forms-request-lifecycle.md) |
| Authentication and tenant/resource access | [Authentication](authentication.md), [scoped model binding](scoped-model-binding.md), [browser sessions](browser-sessions.md) |
| Background work and committed effects | [Jobs](jobs.md), [scheduler](scheduler.md), [transactional outbox](outbox.md) |
| Integration tests | [Testing tools](developer-tooling-and-testing.md), [isolated HTTP database tests](isolated-http-tests.md) |
| Generated clients | [Client contracts](client-contracts.md) |
| Deployment and upgrades | [Operations](production-operations.md), [logging](logging.md), [compatibility](../compatibility.md) |

Resolve configured services in constructors and retain concrete dependencies in
domain handlers. Use the existing named-service settings rather than rebuilding
pool, cache, disk, mail or client factories. Keep transport DTOs separate from
persistence models. The fixtures demonstrate public APIs; their test credentials,
fake guards, loopback providers and domain names are not production defaults.

Scaffolding targets an existing Go package and preserves its declared package
name. Initialize each new package once in your chosen application layout. For
this new-project example:

```sh
mkdir -p models transport
printf '%s\n' '// Package models owns persisted domain declarations.' 'package models' > models/doc.go
printf '%s\n' '// Package transport owns public request and response contracts.' 'package transport' > transport/doc.go
go tool foundry make model Widget --table widgets --dir ./models
go tool foundry make dto WidgetResponse --dir ./transport
# Add domain fields and handler behavior to the handwritten files.
go tool foundry generate --recursive --dir .
go mod tidy
go test ./...
```

Model generation supplies typed APIs; it does not create database tables.
Declare reviewed forward migrations for your models and include the enabled
framework features from `app.Migrations()` in your explicit migration workflow.
Ordinary application boot never applies schema changes. Configure PostgreSQL for
persistence and Redis where distributed jobs, scheduling or shared coordination
are required. Select persistent session/token services for production authentication.

Keep generated output and `.foundry-gen.json` ownership manifests alongside their
declarations. Regenerate after declaration or framework changes. A failed
generation check lists each stale file and whether its content, comments,
formatting, managed field notes or ownership manifest differ. Add these checks
to the boilerplate's own CI, together with its configured integration/client tests:

```sh
go tool foundry generate --recursive --check --dir .
go vet ./...
go test ./...
go test -race ./...
```

The framework repository's `make verify` also exercises its independent fixtures,
compiler rejection and generation checks. It is a framework maintainer command;
your application CI owns your domain tests. Select an existing `gopls` with
`go tool foundry doctor --gopls /path/to/gopls --require-gopls` when validating editor
setup. [Agent tooling](agent-language-tooling.md) queries the actual consumer graph.

## Adoption boundaries

The [team readiness audit](team-readiness-20260925.md) records current acceptance.
PostgreSQL is the supported database. Redis connections target explicit standalone
servers; Cluster and Sentinel routing are not implemented. MySQL/SQLite, Dart client generation,
durable WebSocket recovery and Pusher/Echo compatibility remain
[deferred extensions](../../blueprint/25-deferred-extensions.md).
Type checking protects declared Go contracts; validation, authorization and
database constraints still enforce runtime input and business rules.
The local-storage and file-cache adapters currently require macOS or Linux.
Native acceptance was performed on macOS; run the application's integration and
load tests on its actual deployment platform before a production rollout.

Start the team's boilerplate with one real vertical slice: configuration, a
persistent authenticated actor, an authorized typed endpoint, a forward migration
and an isolated HTTP/database test. Use the existing bounded defaults and measure
the team's own workload before selecting production capacity. Historical local
[measurements](developer-resource-measurements.md) describe their tested profiles,
not an application throughput guarantee.
