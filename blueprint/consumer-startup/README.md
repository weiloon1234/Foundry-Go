# Consumer startup and typed configuration

This continuation builds the consumer experience on the accepted framework
foundation. It implements the direction agreed on 2026-09-17: small application
bootstrap code, framework-owned infrastructure, typed Go contracts, named services
with automatic defaults, and native IDE completion.

The [master](../00-master-architecture-and-parity.md#consumer-startup-delivery)
owns status. This directory owns the detailed contracts. The
[experience audit](../../docs/framework-experience-audit.md) provides the source
evidence behind this work. This remains a framework project: executable consumer
fixtures prove the API; no starter application is created.

Read and implement in order:

1. [Typed configuration and generation](01-typed-configuration.md)
2. [Named services and default selection](02-named-services.md)
3. [Application and HTTP assembly](03-application-assembly.md)
4. [Supporting service integration](04-supporting-services.md)
5. [Consumer acceptance and final audit](05-acceptance.md)

## Scope and invariants

- Consumer Go structs and declarations own types, names, defaults and behavior.
  Generated descriptors derive from those declarations; TOML and environment
  supply deployment values, not Go type declarations or executable callbacks.
- Reuse the existing generator, configuration loader, service graph, registries,
  feature modules, owned runtime, database/query engine and adapters.
- PostgreSQL is the only database adapter. Required additional cache adapters
  are PostgreSQL and file; these do not imply a second database engine.
- Configurable services expose a default and typed named alternatives. A default
  aliases the same service instance and is immutable within one application.
- Shared services/configuration are injected. Context carries cancellation and
  request attribution; authenticated handlers retain concrete model arguments.
- Build validates without opening external resources. Run/Start boot configured
  resources once; cleanup remains reverse ordered, bounded and application owned.
- No runtime model-directory scanning, runtime Go compilation, process-global
  service singleton, dynamic actor casts or automatic schema migrations.
- Complete a milestone's code, test sources and docs before compilation/testing.
  Collect failures, batch fixes, rerun affected checks, then complete its required
  full gate. Do not compile after every minor edit.

Ordinary filesystem manipulation, generic browser session data and server-rendered
views remain separately scoped extensions from the audit. This continuation must
integrate delivered uploads, object storage, imaging and browser authentication;
it does not redefine those existing facilities as those additional extensions.
