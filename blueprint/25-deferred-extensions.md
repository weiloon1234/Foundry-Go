# 25 — Deferred extensions

## Purpose and prerequisites

These capabilities are recorded for future work and are not gates for the first complete framework release. Each requires a separate implementation milestone after its base contracts are stable.

Rust references: `src/database/compiler.rs`, `src/websocket/mod.rs`, `src/typescript/mod.rs`, and deferred notes in `blueprints/14-websocket-system.md` and `20-rust-ssot-contract-generation.md`.

## MySQL and SQLite

Extend the existing query AST through dialect compilers and execution adapters. Do not fork the model/generation API. Document capabilities and differences for returning rows, upsert, locking, JSON, exact decimals, timezones, migrations and transaction isolation.

Planned consumer contract: the same generated `QueryUsers()` and mutation APIs operate against a supported dialect, while unsupported capabilities fail explicitly. Add a real-database matrix before claiming parity. SQLite must not become a substitute for PostgreSQL integration testing.

## Durable WebSocket recovery

Design retained event storage, monotonic scope-specific cursors, authenticated resume tokens, reconnect authorization, retention expiry, replay/live handoff and client deduplication. Bounded replay from the initial release must not be retroactively described as durable recovery.

Planned capability: a typed client resumes an authorized stream from a validated cursor and receives an explicit reset/resync response when history has expired. Delivery and ordering guarantees require a separate protocol/storage design and multi-instance failure tests.

## Dart contracts and SDK

Add a Dart emitter over the existing normalized manifest. Preserve nullability, named identities, exact numeric/date wire formats, files, errors and realtime event direction. Do not create a second schema source or generate a Flutter application.

Acceptance requires compiled Dart output and transport contract tests against the same HTTP/realtime fixtures as TypeScript.

## Backup and certificate failure alerts

Status: proposed and deferred by the owner on 2026-11-05. Nothing is implemented, and three decisions below are open.

Backups run from systemd timers, a differential daily at 05:00 UTC and a full on Sunday at 02:00 UTC. Certificate renewal runs from its own timer. No deploy tool code sends an alert: `backup_stale`, `backup_failed`, `units_failed` and `certificate_expiring` appear only when someone runs `ACTION=report`.

An alert has to catch a run that fails, such as bad storage keys or a full disk, and a run that never happens, such as a stopped server or timer. A stopped server cannot report itself, so the second case needs a monitor outside it.

| Option | Run fails | Never runs | Trade-off |
| --- | --- | --- | --- |
| Outside check-in service (recommended) | Yes | Yes | Relies on a third party, which sees only check-in times |
| Server sends its own message | Yes | No | The server must hold a Telegram token or email login, and has no mail tool today |
| Check from a developer machine or hosted CI | Yes | Yes | A laptop sleeps, and hosted CI would need SSH access to production |

Recommended service: [Healthchecks.io](https://healthchecks.io/pricing/). Read on 2026-11-05, its free plan monitors 20 jobs and its [pinging API](https://healthchecks.io/docs/http_api/) has a `/fail` endpoint that signals failure immediately. Check both again before building.

Planned design:

- Each server gets an optional check-in URL, supplied through a private file like the other deploy secrets. The URL never appears in logs, receipts or reports.
- A scheduled backup checks in after its database backup and key archive complete, and calls `/fail` when it fails. Check-ins have a bounded timeout, and a failed check-in never changes the backup's result.
- Both backup kinds share one check per server, expected daily with about two hours' grace, so silence raises an alert after about 26 hours.
- `ACTION=report` shows whether a check-in is configured and how the last one went, with a problem code when it failed.
- This ships as a new tool generation. Confirm in `ACTION=tools-plan` that no application service restarts.

Acceptance: unit tests and container acceptance against a local fake monitor cover success, failure and an unreachable monitor, and prove the URL stays out of logs and reports. Then trigger a real alert on staging before upgrading production.

Open decisions:

1. **Service:** an outside check-in service, or a message from the server, which misses a stopped server.
2. **Destination:** chosen in the service, not in code. Email is the simplest. Telegram is wanted but was not confirmed on the pages read above.
3. **Certificate renewal:** the same mechanism can cover it with one more check per server. This is recommended, because a renewal that keeps failing eventually takes the site down.

A completed backup does not prove it can be restored. Restore drills (`ACTION=restore-drill`) stay a separate check.

## Other future requests

Binary WebSocket messages, Pusher/Echo compatibility, additional storage/database/message brokers, and domain-specific packages require explicit scope decisions. Do not introduce them as hidden prerequisites for Rust parity. The future **Foundry-Go-Starter** remains outside this framework roadmap and repository.
