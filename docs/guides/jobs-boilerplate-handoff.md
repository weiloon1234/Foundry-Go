# Jobs integration handoff for the team boilerplate

Framework implementation accepted on 2026-09-28. The final `make verify` gate and
runtime/consumer race checks passed on identical source snapshots, with PostgreSQL,
Redis, TypeScript and real gopls required for their applicable checks. See the
[verification record](../evidence/jobs-operations-20260928.json).

These changes are local and unpublished. The repository owner must make this
revision available before the boilerplate pins it. A temporary local Go module
`replace` can support integration against this checkout; remove it when selecting
the published or otherwise accessible reviewed revision. Do not assume an older
framework version contains these APIs.

Use the [worker operations guide](jobs-operations.md) as the API/behavior owner.
The framework now owns failure logs, failed-job inspection and token-guarded
manual retries; the boilerplate should wire these APIs instead of recreating them.

## Application integration

1. Pin the verified framework source and regenerate configuration with its matching
   project CLI. Keep generated files and ownership manifests. The new key is
   `SettingsConfigKeys().Worker.Config.FailureLog`; its default is true.
2. Configure a named Redis-backed job connection using the existing service settings.
   Select it in `Worker.Connection`, configure queues/concurrency and run the same
   application's registrations through `foundation.Worker`. HTTP/CLI instances can
   dispatch without running a worker kernel. The memory backend is for local/tests.
3. Keep concrete payload DTOs and `jobs.Define[Payload]` declarations. Use
   `application.Job` for ordinary handlers, or `application.JobWith` when the
   constructor also supplies typed middleware/admission. Retain injected services
   in handlers; do not serialize them or whole database models into payloads.
4. Register `jobs/command.Declaration` in the existing application CLI registry. Its
   constructor resolves `application.FromResolver(resolver)` and returns
   `services.Jobs`. Use the existing `cli.Module` lifecycle. Expose `jobs failed`,
   `jobs inspect` and `jobs retry` on the application binary; no new Redis client
   or worker loop is needed for these commands.
5. Keep structured failure logging enabled. Add domain diagnostics through typed
   `Failed` middleware only when needed, selecting safe fields. Do not log the
   raw payload or arbitrary returned error. File sinks already own rotation.
6. Use the transactional outbox for jobs tied to business commits. Handlers must
   tolerate at-least-once delivery and use the stable job ID or a domain key for
   side-effect idempotency. Explicit manual retry preserves the same job ID.
7. Configure the deployment supervisor to restart failed worker processes and
   allow graceful draining. Honor cancellation in handlers and bound external I/O.

Use the framework defaults in the [operations guide](jobs-operations.md) as a
starting point. Review concurrency, queue capacity, timeouts, retention and
deployment shutdown grace for the application's workload.

## Application acceptance

Exercise the real selected Redis connection: dispatch from the HTTP/CLI process,
consume through the worker process, force an ordinary transient failure and check
the retry/backoff, then force terminal failure and inspect its safe metadata.
Retry using the saved token, rerun that exact command and prove it does not execute
twice. Check another named connection remains isolated. Confirm logs correlate
with the job and omit payload/credentials, and that shutdown drains before closing
Redis/database/logging dependencies.

Commands operate only on retained independent jobs. They require deployment-level
operator authorization. Workflow-member replay needs an application workflow
decision; the framework rejects reopening an individual member. After an uncertain
network/output result, reuse the same saved retry token to reconcile. Fetching a
new token and repeating the command is a new operator action.

Reference implementations:

- [Typed registration and CLI declaration](../../tests/fixtures/consumer/background/operations.go)
- [Configured worker, named connection, failure hook, logs and CLI retry acceptance](../../tests/fixtures/consumer/background/operations_test.go)
- [Shared memory/Redis retry contract](../../internal/jobtest/retry.go)

## Restricted deployment commands

Register `application.DeployJobCommands(builder)` in a separate CLI path before
`Builder.Build`. It provides `deploy:jobs-inspect` and `jobs` operations, including
`jobs migrate-layout`. Run the parsed invocation with a nil resolver. These
commands construct no business handlers and start no application kernels,
databases, outbox relays, or logging sinks. Restricted migration uses a discard
logger for lifecycle records and opens only the Redis pools used by job connections.

The consumer entry point owns global `--config` and `--env-file` parsing and
loads both through the existing typed settings schema, including `_FILE`
resolution, before passing command-local flags to the restricted registry:

```sh
artisan deploy:jobs-inspect --format json --config config/deploy.toml --env-file app.env
artisan jobs migrate-layout --connection primary --queue delivery --confirm --format json --config config/deploy.toml --env-file app.env
```

Inspection writes one schema-1 JSON envelope. Each connection's `namespace`
contains exactly `application` and `environment` in lowercase. Inventory includes
connection defaults, worker subscriptions, typed job policies, queued listeners,
event outbox groups, and extension cleanup destinations. A blank listener
connection selects the default job connection. A blank listener queue selects
`default`, even when the connection's default queue differs. An unknown explicit
connection produces an `invalid_configuration` blocker. Custom providers and
plugins produce `opaque_job_provider`; declared coverage does not discover
runtime-only persisted queues.

Migration success writes exactly `{"queue":"delivery","migrated":true}` or the
same object with `migrated:false`. Both values confirm a migration call. Reinspect
to establish layout 3. See the [restricted consumer entry point](../../tests/fixtures/consumer/bootstrap/deploy_jobs.go)
and its [process acceptance](../../tests/fixtures/consumer/bootstrap/deploy_jobs_test.go).
