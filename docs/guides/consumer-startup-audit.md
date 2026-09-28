# Consumer startup implementation audit

The C05 pre-audit native `make verify` gate passed in 534.1 seconds. The following
review then covered the changed handwritten implementation from C01 through C05,
compared with the accepted milestone-24 foundation. The final post-audit gate passed in 494.9 seconds, followed by independently
packaged measurements, signal checks and security review. C05 is accepted.

## Scope and method

The comparison inventory contains 127 changed handwritten implementation files
in 24 groups, 23 generated files and 54 test/compiler-fixture files, plus two
removed platform-lock files whose implementation moved to `internal/filelock`. The review
read the handwritten changes, followed their existing lifecycle/adapter boundaries,
and checked requirement coverage against test assertions and the full native gate.
Generated output is checked by deterministic regeneration rather than manual edits.
This is one complete change review, not proof that every possible fault is absent.

| Reviewed area | Findings and retained guarantees |
| --- | --- |
| Config, TOML and generator | One Go declaration drives schemas/keys; typed owners and scalar widths remain intact. Shared loader owns precedence, secret-safe diagnostics, snapshots and table decoding. Generator reuses collision checks and atomic publication. Empty TOML handling was checked against the pinned parser, which initializes empty maps; no speculative decoder change was needed. |
| Named registries and settings | Defaults alias the selected instance; missing names fail. Family-specific types, frozen collections and explicit backend references remain. |
| Infrastructure and cloud | Construction validates before external acquisition. PostgreSQL-only databases, optional read topology, shared Redis and explicit cloud credentials retain existing adapter contracts. S3/R2 configuration proof uses real signing with inert credentials; no new live-account certification is claimed. |
| Persistent cache | Shared expiry/counter semantics, confined files, process locking, integrity bounds, primary PostgreSQL transactions and explicit migrations remain. File/PostgreSQL adapters do not advertise unsupported tags, batches or distributed fills. |
| Application lifecycle and logging | Constructors resolve concrete dependencies; runtime access uses the frozen resolver. Owned sinks/adapters shut down after borrowers, borrowed providers are not closed, and cancellation does not hide joined failures. |
| HTTP and auth | Global middleware covers misses; completion retains request ownership and excludes credentials/payloads. Cookie guards require CSRF. Actor models remain concrete through handlers and browser/token APIs. |
| Supporting services | Named mail/jobs/HTTP/pubsub/realtime reuse one selected worker/realtime kernel. Outbox and audit preserve exact-pool transaction identity and schema restoration. Notifications, reports, model extensions, readiness and observations reuse existing managers. |
| Consumer and release tooling | Compact/full commands use the public assembly API. Format-2 packaging keeps ordinary/configured/full profiles independent, verifies source ownership and excludes local replacements. Measurements separate setup, cold/incremental builds, editor work, startup and request costs. |

## Findings and improvements

1. **Retained audit dependency (C05 implementation finding):** the C04 helper
   performed resolver access at operation time. C05 replaced it with a concrete
   `audit.Scope` resolved in the constructor. The native consumer regression
   exercises the constructed handler after resolver sealing, exact handle identity,
   business rollback, restored schema and rejection of another database pool.
2. **Expected cancellation (C05 verification finding):** the executable smoke
   initially treated ordinary caller cancellation as failure. It now collects
   `Shutdown` failures independently before ignoring expected `Run` cancellation.
   A regression proves a failure joined with cancellation is retained.
3. **Entry-point consistency (post-verification review):** the larger bootstrap
   command still printed normal interruption as failure. Both serve commands now
   follow the verified cancellation handling and use the application's configured
   shutdown timeout for their final wait. Framework lifecycle semantics are unchanged.
4. **Current-status accuracy (post-verification review):** the master still called
   accepted milestone-24 database routing unverified. Its current disposition now
   links the actual production acceptance; historical progress entries remain intact.

The final audit batch adds no dependency, runtime service registry, duplicate
configuration schema or alternative infrastructure path. The existing meaningful
cancellation/lifecycle regressions cover the reused shutdown contract; the final
full gate compiles both commands and repeats consumer, compiler, editor,
generation, native-service and documentation checks.

## Verification and evidence boundary

C05 focused native/race checks passed for audit/application/infrastructure and the
compact/full bootstrap consumers before the pre-audit gate. Release tools, format-2
manifest tests and benchmark execution checks passed. The short benchmark smoke
is an execution check only; final performance conclusions require the isolated,
repeated native candidate measurements.

Final gate, source hashes, packaged consumer and security results are recorded
in the [acceptance guide](consumer-startup-acceptance.md) and
[acceptance evidence](../evidence/consumer-startup-c05.json). Both packaged serve
commands also answered HTTP and exited successfully on SIGINT using a two-second
configured shutdown timeout. No source was committed,
pushed, merged or published as part of this work.
