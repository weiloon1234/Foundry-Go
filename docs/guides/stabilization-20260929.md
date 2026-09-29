# Stabilization review — 2026-09-29

This is the independent follow-up to the [framework improvement program](improvement-program-20260929.md),
before the team pins its boilerplate to the revised framework. It covers changed
authentication, transaction retry, outbox/jobs, generated contracts and dependency
consumption. The bounded review is complete: the corrected framework and its
independently packaged consumers passed the checks below. The team can continue
its boilerplate against a reviewed, retrievable revision after the upgrade steps
in this guide. No revision was published, and the actual starter remains unchanged.
Deferred parity features and production infrastructure certification are outside
this review.

## Corrected findings

| Finding | Correction and regression coverage |
| --- | --- |
| Query cancellation could close a returned row stream while its cleanup hook was still being assigned | Register the hook under the existing row mutex; cancel during driver return, verify single close and connection reuse, and repeat real queue CLI processes |
| Cache load counts included fills that skipped the loader after rechecking storage | Increment at the shared loader invocation; deterministic externally-filled recheck and concurrent stale-refresh coverage |
| Nil publisher receivers were dereferenced before validation | Shared initialization check; nil/zero receiver and nil-context regression cases |
| Retry inspected custom errors outside isolation and missed later joined outcomes | One bounded isolated traversal, conservative outcome precedence, cyclic/panic/Goexit cases, saturating backoff |
| Typed policy denials were inspected after scope ownership ended | Inspection inside owned policy/hook execution; wrapped denials, failed inspections and scope-close ownership coverage |
| Impersonation resume listed, revoked and replaced actor sessions separately | Checked transactional consumption and replacement; revocation, rotation, expiry, rollback and concurrent-resume PostgreSQL cases |
| OAuth JSON trailing delimiters and undersized RSA moduli could pass shape checks | Require JSON EOF and actual 2048-bit modulus minimum; focused parser/key tests |
| Idempotent JavaScript fixture still expected manifest version 4 | Native fixture receives the current Go version constant; replay and mismatch assertions remain unchanged |
| Scheduler capacity test queued extra wakes while assuming an observable idle interval | Wait for the tick and callback exit deterministically; retain admission and capacity-expiry assertions |

The revised publisher loop also has deterministic shutdown coverage: a cancelled
managed task stops cleanly even when its driver error does not wrap cancellation.
One-shot publication still reports the original failure and never claims commit.
This covers the loop boundary implicated by the starter's recorded B07 issue;
consumer process checks provide separate application evidence.

## Verification and evidence

The [acceptance record](../evidence/stabilization-20260929.json) contains source
hashes, exact commands, exit statuses, package integrity, scanner results and
retained evidence locations. Tests required the existing PostgreSQL and Redis
services and used isolated retained namespaces. No database was reset, and no
dependencies or tools were installed or upgraded.

| Check | Result and boundary |
| --- | --- |
| Final framework `make verify` | Passed after the last runtime fix: formatting, vet, tests, consumer fixtures, generation, documentation and release-tool checks |
| Race coverage | Full `make race` passed before the final row-cleanup fix; the final source then passed affected database, PostgreSQL, publisher, foundation, application and HTTP races plus six consumer integration packages |
| Row cancellation regression | The new test reproduced the old data race in an isolated pre-fix package; the fixed version passed 4,000 cancellation cases with automatic connection release, one driver close and connection reuse |
| Cache and scheduler regressions | Each corrected concurrency case passed 100 race-enabled repetitions; affected package race suites also passed |
| Generated contracts | Real HTTP/WebSocket TypeScript checks passed; a 20-second manifest decoder fuzz campaign completed 3,871 executions without a failure |
| Independent module consumption | Three packaged consumer profiles downloaded through a private module proxy with an isolated module cache and no local replacements; generation was reproducible, module integrity passed, and the full profile built with targeted integration tests |
| Starter on macOS and Linux arm64 | Disposable upgraded copies passed `scripts/verify.py --independent --require-integrations`, including full Go race tests, generated-client checks, vet, generation checks, module integrity and compiled CLI checks |
| Queue process stress | Ten additional race-enabled `TestJobsOperationsAcrossProcesses` repetitions passed on each platform, covering retry/backoff, inspection/retry, queue isolation and worker shutdown |
| Linux deployment check | The actual systemd unit passed offline validation; the compiled HTTP command ran as a non-root user with an empty PATH, became ready, handled SIGTERM/SIGINT with exit 130 and released its listener |

The initial packaged candidate passed a single starter run but failed repeated
queue process testing. Captured race stacks identified the row cleanup-hook
initialization defect; the final candidate was rebuilt after that correction and
passed both platforms' repeated checks. Superseded results remain in the record.
Only reporting files changed after the final framework gate; the acceptance
record verifies that runtime and test inputs stayed unchanged.

Security checks found no affected runtime functions. The existing module-only
`GO-2026-5932` advisory remains; none of the packaged consumer graphs imports the
affected OpenPGP packages. Existing gopls module-only advisories and their binary
scan results are recorded separately. The TypeScript lockfile audit had no
findings. All 57 external modules retain the previously reviewed versions and
license/notice hashes; see [dependency review](../dependency-review.md).

Linux evidence covers the starter on arm64 in the existing container image. It
does not cover the full framework suite on every Linux architecture or live
systemd supervision, restart policy, sandbox enforcement and journald. Real
provider-account email sends and live S3/R2 certification were not repeated.

## Starter migration findings

The real starter still pins `v0.0.0-20260928094130-4cc5fdeb78b6`. Acceptance used
disposable copies and a private candidate; it did not change that repository.
The handwritten migration patch is retained at
`.cache/stabilization-20260929/starter-handwritten-migration.patch` in the
framework checkout. The copies needed these updates:

- Password login lockout declarations now use `lockout.DefineLogin` with
  `lockout.DefaultLimits`. This is required by `password.WithLockout`; keep trusted
  client/proxy configuration explicit as described in [login lockout](login-lockout.md).
- Migration assertions derive expected keys and states from the actual framework
  and project declarations. The token migrations increased the starter profile's
  total from three to four; production upgrade code must run the new migration.
- The scheduler test explicitly sets `CapacityWait = 0` when testing immediate
  rejection. The runtime default now waits for capacity.
- Pagination tests expect `/query/page` when the new maximum-page bound fails,
  including values that previously reached the later offset-overflow check.
- Go output, manifest version 5, OpenAPI and TypeScript were regenerated from
  declarations. The generator and framework must use the same selected version.

These changes preserve the starter's authentication, DTO privacy, migration,
worker and validation assertions. The saved patch contains handwritten changes;
regenerate outputs after choosing the public framework revision.

## Boilerplate upgrade order

1. Pin the reviewed framework revision and use that same requirement for the
   `foundry` generator tool. Preserve generated ownership manifests. A private
   candidate is acceptance evidence; it is not a public revision the team can pin.
2. Regenerate Go output, saved contract manifests, OpenAPI and TypeScript together.
   The contract manifest is version 5; update imports affected by schema names.
   Tolerant response decoding still returns only declared fields. Use explicit
   response DTOs/projections; never expose persistence models wholesale.
3. Apply the configured framework's forward migrations before starting processes
   that read the new schema. This includes attachment variants when attachments
   are configured. Bootstrap does not migrate automatically.
4. For existing Redis job queues, stop/drain all old workers, dispatchers and
   operators, then run `jobs migrate-layout --confirm` for each configured queue
   and connection as described in [jobs operations](jobs-operations.md#redis-queue-layout-upgrade).
   Upgrade all envelope readers before enabling format-3 features. Fresh queues
   do not need legacy-layout migration.
5. During mixed-version deployment, use new cache/rate-limit namespaces and retain
   the old explicit WebSocket `MaxConnections`, or use a new realtime namespace.
   Review [compatibility](../compatibility.md) before choosing rollback behavior.
6. Exercise the boilerplate's own authentication, validation, DTO/client and worker
   tests against the selected version. Authorization now precedes validation;
   authorization code must tolerate structurally valid but unvalidated input.
   Server-side timeout failures use 503; 408 is reserved for input-read/decode
   timeouts. Configure a durable queue outside local/test, or explicitly accept
   the in-memory driver's limitations.

Custom session adapters need `session.ResumptionBackend` for impersonation resume.
The built-in PostgreSQL adapter supplies it. This adds no new schema migration.
Token impersonation, cache failover, direct sendfile, lossy WebP, Go-comment schema
descriptions and the four remaining scaffolds remain outside this pass.
