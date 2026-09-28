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

## Other future requests

Binary WebSocket messages, Pusher/Echo compatibility, additional storage/database/message brokers, and domain-specific packages require explicit scope decisions. Do not introduce them as hidden prerequisites for Rust parity. The future **Foundry-Go-Starter** remains outside this framework roadmap and repository.
