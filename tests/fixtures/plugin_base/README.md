# Independent base plugin fixture

This is a library fixture, not an application starter. Its own Go module imports
only public Foundry packages, and the dependency fixture imports it as an ordinary
module. `GOWORK=off` applies to independent checks.

The plugin loads namespaced typed configuration and owns shared HTTP, auth,
migration, event and job services. Existing feature modules register directly
with the plugin registrar. The event bus drains before the owned memory job
backend closes. `testkit.Plugins` exercises the production plugin lifecycle.

Historical migrations intentionally execute harmless SELECT statements; real
PostgreSQL consumer acceptance checks their retained migration history, semantic
introducing versions, dependency order and idempotency without resetting data.

This fixture passed milestone 22 ordinary and race acceptance.
`make fixture-check` and `make race` at the repository root include it.
