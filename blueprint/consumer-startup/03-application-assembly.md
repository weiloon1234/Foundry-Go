# C03 — Small application and HTTP bootstrap

Prerequisite: C02. Introduce a framework-owned assembly layer over the existing
foundation; feature packages must not import root assembly. Preserve the existing
advanced `foundry.New().Register(...).Build(...)` API.

## Bootstrap

The ordinary path loads typed configuration, registers typed application
declarations, builds, then runs a selected kernel. It installs configured built-in
providers and validates their complete graph. Standard infrastructure must not
require consumers to define service keys, adapter factories, resolver closures,
shutdown code or third-party imports.

Choose and compile-check concrete builder names in the independent consumer.
Keep configuration-file selection, typed programmatic settings, routes, guards,
custom middleware, domain modules and plugins explicit. Build returns ordinary
errors and freezes defaults/configuration. Run/Start use the existing lifecycle.
Do not introduce runtime model-path discovery or automatic database migration.

Application-specific configuration stays concrete. Generic top-level helpers may
bridge generated settings into assembly; do not require generic methods that Go
does not support or erase custom settings to `map[string]any`.

## HTTP and authentication

- Assemble the owned router/server and standard request handling from config.
  Consumers continue declaring concrete path/query/body/response contracts.
- Supply standard browser/API policy composition with documented middleware
  order. Cookie authentication requires CSRF protection even on API-named routes;
  bearer-only policy must not inherit unnecessary cookie state.
- Custom global middleware wraps route misses and method failures too. Preserve
  standard writer capabilities, cancellation and ownership.
- Access logging observes completion, status, duration, request correlation and
  errors. Config selects its sink. Credentials/bodies are not automatic log data.
  Expose typed observation/lifecycle hooks without a competing pre/post pipeline.
- Guards/providers retain model types. A group may choose a default typed guard;
  named alternatives for other models create correspondingly typed handlers.
  Authentication and resource authorization remain separate.
- Shared services and immutable config enter constructors; `context.Context`
  carries cancellation and request attribution. Do not add a global context bag.

## Consumer proof

Add a small independent test application with real `main`/bootstrap entrypoints,
domain handlers and generated configuration. It must exercise HTTP, two actors,
PostgreSQL, cache, uploads/storage and imaging without native SDK imports or
consumer-written infrastructure factories. It remains a framework fixture, not a
starter product. Include different-config and parallel-app tests.

Run service-free configuration failures, real transport authentication/CSRF and
middleware-order cases, graceful shutdown, compiler/editor checks and the complete
native gate. Compare imported package/build cost with the accepted ordinary
consumer; runtime-disabled features still have an import-time compilation cost.

## Concrete implementation contract

- `application.Settings` nests the existing infrastructure and HTTP server settings,
  optional image ownership, an owned JSON logging sink and shutdown timeout. Its
  schema/keys use the same generator as consumer settings.
- `application.New(settings).HTTP(routes).Use(middleware).ObserveHTTP(observers)`
  retains explicit Go declarations; `Register`/`RegisterPlugin` preserve the advanced
  graph. Build returns an App embedding the existing foundation lifecycle.
- Route constructors receive `application.Services` and return already typed route
  registrations. Existing router contributions are merged, not bypassed.
- `http.BindGuard` creates one concrete default binding; `http.Authenticated` retains
  the handler actor type. Cookie authentication carries CSRF policy into every route.
- Completion observes the existing transport writer and shared outcome classifier.
  It does not create a second request lifecycle or retain request payloads.
- `tests/fixtures/consumer/bootstrap` owns the executable proof and generated models,
  DTOs, multipart input and configuration. Native acceptance is still required.
