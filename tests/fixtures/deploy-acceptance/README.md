# Deploy acceptance consumer

This independent application supplies the archive for the deploy tool's systemd
container acceptance. Its module uses `v0.0.0` with a local replacement for native
tooling. The acceptance builder packages this checkout into a private module proxy
and rewrites only an isolated fixture copy to require that candidate without a
replacement. One archive serves both the published generation-9 baseline tools and
the candidate in the upgrade phase. It is not a starter application.

The topology declares HTTP, one Redis worker consuming `first` and `second`, and
a scheduler that logs `acceptance schedule completed` every ten seconds. The
configuration declares private R2 attachments, PostgreSQL migrations and a seeder,
a Redis coordination child, and the `portal-runtime` cache child. The container
bootstrap supplies credentials and rewrites the R2 disk to its private TLS S3
stand-in. No credentials are stored in this fixture.

From the repository root, build the archive with:

```sh
info=$(mktemp -d)
python3 tools/deploy/tests/build_acceptance.py --architecture arm64 --archive-file "$info/archive.txt"
```

The helper calls the existing deploy builder. Its Node 24 `web` package has no
third-party npm dependencies. The builder runs `npm ci` then `npm run build` in
Docker with network access available. The fixture also builds with npm offline.
The build emits a readiness page and unchanged browser JavaScript in `public/`,
plus a real line-for-line source map under
`storage/private-source-maps/$FOUNDRY_BUILD_RELEASE/assets/portal.js.map`.
The builder copies that version's maps into the private release tree; public
assets contain no map or source-map URL. These assets exercise release packaging;
the container scenarios use the app's health endpoint, not portal navigation.
See [the deploy test instructions](../../../tools/deploy/README.md) for the
container cells and recovery flag.

The test controller calls the installed release's CLI through `release.app`:

```sh
artisan fixture:enqueue --queue first --id 22512288-ed02-4faa-92f1-b2c69ed383e1 --barrier --config config/deploy.toml --env-file app.env
```

The command dispatches a real typed Redis job without running service kernels and
returns JSON containing its domain `id`. The worker writes
`hostdrill/<id>.started` under its working directory, waits for
`hostdrill/<id>.release` when `--barrier` was selected, and then writes
`hostdrill/<id>.completed`. The domain identity must be a canonical UUIDv4. Handler
cancellation prevents a completion receipt; normal worker shutdown allows twenty
seconds for admitted work before cancellation. A killed worker's lease expires
within the normal thirty-second lease window before another worker can retry it.
The controller owns removal of its exact markers.

`[fixture] readiness` accepts `healthy`, `fail`, or `hold`. The HTTP command creates
`hostdrill/readiness.started` for `hold`; `/health/ready` remains unavailable until
`hostdrill/readiness.release` exists. Configuration inspection and readiness
checks do not create markers. The recovery harness makes immutable config variants
and clears the flat readiness markers before each held-readiness scenario.

The fixture mirrors Starter's typed `/health/live` (`{"status":"ok"}`) and
`/health/ready` (`{"ready":true}`) responses with `Cache-Control: no-store`.
Readiness uses the diagnostics runtime, including lifecycle and the fixture's
fail/hold probe. The built-in `/ready` route registers that runtime without a
custom provider that would make job inventory opaque. Its separate
`{"status":"ready"}` wire shape is also accepted by the deploy tool.

The CLI delegates configuration, jobs operations, Redis grants, storage
probes, database migration and seeding to their public framework owners. The
`redis:keys` adapter emits JSON without flags, as required by the deploy tool.
Its migration creates `acceptance_fixture`; its idempotent seed inserts one fixture
row. It never resets a database.

The console contract was compared with Starter: `config:check` returns
`{"valid":true}` without booting services; `config:inspect` reports sources only;
storage probes use `application.ProbeStorage`; migration and seed commands use the
database command owner with JSON output. Worker startup requires the configured explicit
connection. Scheduler output matches the runner's expected message and maximum
interval.

Schedules are declared only when `app.scheduler.enabled` is true, matching
Starter and allowing replacement-host rehearsal to disable the scheduler while
still validating the full application graph with `config:check`.

Fixture development, from this directory:

```sh
GOWORK=off go tool foundry generate --dir internal/config
GOWORK=off go tool foundry generate --dir internal/app
GOWORK=off go test ./...
GOWORK=off go build -o /tmp/foundry-deploy-acceptance ./cmd/artisan
node --test web/build.test.mjs
```

## Large-upload acceptance

An explicit `app.http.uploads.enabled` profile adds `/acceptance/uploads/large`,
`short`, `small`, `multipart`, `usage` and `cleanup`. The existing fixture table supplies typed
owners; attachment migrations add the framework journal. The fixture always
declares its extension owner and extension/attachment migrations, including when
uploads are disabled. Normal release activation applies this same migration set.
The uploads phase only reads migration status: it requires identical history
under both environments and checks that history is unchanged after cleanup, so
later backup and recovery phases use the normal release's schema. The serving process
uses a real local disk and attachment manager. Its test-only adapter can hold Put
or local staging at an exact controller-owned release marker. Success follows
Prepare, a committed ReplaceIn transaction and a full digest-verified read.
The short route declares six seconds total. Ordinary deployments keep these
routes disabled. These public test routes are for the disposable fixture only.

The [B26 acceptance record](../../../docs/evidence/b26-resource-provider-acceptance.md)
describes controller markers, all scenarios and lead-run commands. The controller
uses production nginx and systemd renderers under the actual service identity.
It passes the deployment sublimits to the attachment manager and local adapter,
with no fixture semaphores. Small publications use a managed root on the reserved
small volume. Existing consumer fixtures are otherwise unchanged.
