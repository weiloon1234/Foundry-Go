# Rust module reconciliation

This inventory reconciles the master's Rust module rows with concrete Go source,
guides and representative acceptance sources. Rust references were read from
`src/lib.rs`, `src/public.rs` and the subsystem references recorded in each
blueprint. The Go source/package inventory was checked directly. No Rust source
is required by Go consumers.

The [milestone 24 acceptance](../production-acceptance.md) records the completed
native gate, independent packaged consumers and complete-framework audit/fixes.
The table identifies executable evidence sources and deliberate mappings; it
does not promise exact source-language API equivalence.

| Rust module | Go implementation and consumer contract | Representative acceptance source |
| --- | --- | --- |
| foundation | [App/Builder](../../foundation/) and [owned lifecycle](foundation.md) | [lifecycle](../../foundation/lifecycle_test.go), [new observations](../../foundation/observability_test.go) |
| config | [typed schema/namespaces](../../config/) plus feature config structs | [schema](../../config/schema_test.go), [JSON](../../config/json_test.go) |
| kernel | [shared kernel/lifecycle contracts](../../foundation/contracts.go), concrete HTTP/worker/schedule/socket/CLI runtimes | [five-kernel lifecycle](../../foundation/lifecycle_test.go), feature lifecycle suites |
| logging | [slog](../../logging/), [observations/traces](observability.md), [health/diagnostics](production-diagnostics.md) | [logging](../../logging/logger_test.go), [new correlation](../../logging/correlation_test.go), [new exporters](../../observability/exporters_test.go) |
| support | [typed values](typed-values.md), [temporal/support APIs](supporting-apis.md), collection/encryption/randomtoken/sanitize | [IDs](../../model/id_test.go), [temporal](../../temporal/temporal_test.go), feature suites |
| app_enum | [enum descriptors](../../enum/) and [generated declarations](model-generation.md) | [descriptors](../../enum/descriptor_test.go), [labels](../../enum/labels_test.go), generator/consumer gates |
| database | [runtime](database-runtime.md), [models/queries](model-queries.md), [new routing](database-routing.md), [new binary fields](binary-models.md) | [query suite](../../database/query/), [generator](../../internal/generate/), [independent model consumers](../../tests/fixtures/consumer/) |
| events | [typed dispatch and commit boundaries](events.md) | [dispatch](../../events/dispatch_test.go), [after-commit PostgreSQL](../../events/after_commit_postgres_test.go) |
| audit | [typed persisted mutation snapshots](audit.md) | [PostgreSQL audit actions](../../audit/action_postgres_test.go) |
| http | [typed routes](http-routing.md), [DTOs](http-dtos.md), transport/edge/resource APIs | [HTTP suite](../../http/), [new connection cap](../../http/connection_budget_test.go) |
| validation | [typed rules, selection, composition and contracts](validation.md) | [selection](../../validation/field_selection_test.go), [conditional](../../validation/conditional_test.go), generator/compiler fixtures |
| redis | [owned Redis runtime](redis.md), [commands](redis-commands.md) and feature adapters | [real adapter suites](../../redis/) |
| cache | [typed caching](caching.md), [distributed coordination](distributed-cache.md) | [cache](../../cache/cache_test.go), [coordination](../../cache/coordination_test.go), Redis suites |
| support::lock | [owned leases and heartbeats](leases.md) | [leases](../../lease/lease_test.go), [proofs](../../lease/proof_test.go), [release ordering](../../lease/release_order_test.go) |
| auth | [model-first auth](authentication.md), sessions/tokens/password/recovery/MFA/scopes/permissions | [auth](../../auth/auth_test.go), [HTTP scopes](../../http/access_scopes_test.go), PostgreSQL/Redis/provider suites |
| storage | [local/S3/R2 streaming storage](storage.md) | [disk semantics](../../storage/disk_test.go), adapter suites and [live S3/R2 baseline](../../blueprint/00-master-architecture-and-parity.md#milestone-11-acceptance) |
| jobs | [typed jobs/worker/outbox/workflows](jobs.md), [new trace rollout](job-trace-rollout.md) | [jobs](../../jobs/jobs_test.go), [PostgreSQL outbox](../../jobs/outbox_postgres_test.go), [new mixed readers](../../jobs/trace_rollout_test.go) |
| scheduler | [schedule package](../../schedule/), [cron/leadership/overlap](scheduler.md) | [coordination](../../schedule/coordination_test.go), [new maintenance](../../schedule/observability_test.go) |
| websocket | [local realtime](websocket.md), [distributed runtime](websocket-distributed.md) | [context](../../websocket/context_test.go), [protocol](../../websocket/protocol_test.go), [new observations](../../websocket/observability_test.go), Redis suites |
| email | [email drivers, templates, attachments and conservative delivery](email.md) | [email](../../email/email_test.go), [providers](../../email/providers_test.go), [job integration](../../email/jobs_test.go) |
| notifications | [typed database/email/broadcast channels](notifications.md) | [claims](../../notifications/claim_postgres_test.go), [delivery](../../notifications/delivery_postgres_test.go), realtime contracts |
| imaging | [bounded transforms/encoding](imaging.md) | [engine](../../imaging/engine_test.go), [limits](../../imaging/limits_test.go), [decode fuzz source](../../imaging/inspect_fuzz_test.go) |
| attachments | [typed collections, lifecycles and derivatives](attachments.md) | [collections](../../attachments/collections_postgres_test.go), [commit](../../attachments/commit_postgres_test.go), [failures](../../attachments/failures_postgres_test.go) |
| metadata | [model extensions](model-extensions.md), [typed metadata](../../metadata/) | [PostgreSQL metadata](../../metadata/metadata_postgres_test.go), [registry](../../metadata/registry_test.go) |
| translations | [model translations](../../translations/), [shared locales](localization.md) | [PostgreSQL translation](../../translations/translations_postgres_test.go), [cleanup](../../translations/cleanup_postgres_test.go) |
| settings | [typed settings](settings.md) | [PostgreSQL settings](../../settings/settings_postgres_test.go), [contracts](../../settings/settings_test.go) |
| countries | [reference model/data](countries.md) | [reference](../../countries/reference_test.go), [PostgreSQL](../../countries/countries_postgres_test.go) |
| datatable | [typed query-backed tables and exports](datatable.md) | [PostgreSQL behavior](../../datatable/datatable_postgres_test.go), [client metadata](../../datatable/client_metadata_test.go) |
| i18n | [locale/catalog/typed message arguments](localization.md) | [catalog](../../i18n/catalog_test.go), [message keys](../../i18n/message_key_test.go), compiler fixtures |
| http_client | [named bounded clients](http-client.md) | [native transport](../../httpclient/native_test.go), [lifetimes](../../httpclient/lifetime_test.go), [new propagation](../../httpclient/observability_test.go) |
| contract | [typed JSON and normalized manifests](client-contracts.md) | [JSON composition](../../contract/json_composition_test.go), [manifest package](../../contract/manifest/) |
| openapi | [OpenAPI adapter](../../openapi/) over the shared contract | [OpenAPI](../../openapi/openapi_test.go), independent generated contracts |
| typescript | [generator/SDK transport](client-contracts.md) | [rendering](../../typescript/render_test.go), [consumer runtime](../../tests/fixtures/consumer/clientcontracts/) |
| plugin | [typed manifests/contributions/dependencies/assets/scaffolds](plugins.md) | [configuration](../../plugin/config_test.go), [migration history](../../plugin/migrations_test.go), independent plugin modules |
| cli | [typed CLI, scaffold/inspection/doctor tools](developer-tooling-and-testing.md) | [commands](../../cli/command_test.go), [consumer tooling](../../tests/fixtures/consumer/tooling/) |
| testing | [testkit and explicit local helpers](developer-tooling-and-testing.md) | [cleanup](../../testkit/cleanup_test.go), independent fixtures and native compiler/editor/SDK gates |
| prelude, public | [explicit package exports and root assembly](../../foundry.go) | independent consumer imports; no Go prelude |

`foundry-macros` / `foundry-build` map to the source generator and owned publication
checks. `foundry-agent` maps to actual gopls completion/hover/definition. Go package
documentation and compiled consumer examples replace the Rust HTML API extractor.
Rust's `__private` and `__reexports` are implementation mechanisms, not missing Go
features. These mappings preserve typed application responsibilities without
copying a second language's global discovery or re-export mechanism.

## Database follow-up disposition

The earlier master's follow-up list is historical. Typed
[insert from SELECT](model-insert-from-query.md) and
[joined update/delete sources](model-source-writes.md) were completed in milestone
07; their runtime/generator/consumer sources remain in the combined baseline.
[Query plans and explicit execution analysis](query-plans.md) were completed and
accepted in milestone 23. New primary/read routing and generated byte fields are
accepted in milestone 24, with connector/PostgreSQL/ownership/generator/
compiler/editor cases passing its full native verification.

## Deliberate mappings and evidence limits

- Go registries and generated declarations are explicit; there is no Rust
  inventory/prelude/global tracing subscriber. Lifetimes use context plus owned
  `Close`/`Done`, including callbacks that ignore cancellation. Synchronous Rust
  drop cleanup is not copied as detached Go work.
- PostgreSQL is the agreed database target. Raw `Query` remains primary because
  it can execute `RETURNING` writes. Generated read selection uses an explicit
  capability and retains actual transactions. Read-replica tests prove routing
  and unavailable-endpoint handling, not replication-lag guarantees.
- Operational URLs, JSON reports and metric names are Go contracts, not identical
  copies of Rust's diagnostics HTTP schema. Shared observations are bounded and
  payload-free; detailed typed subsystem inspection remains under ordinary
  application authorization. No implicit unauthenticated diagnostics is added.
- Error/trace exporters are callback interfaces. No vendor backend or automatic
  external report delivery is configured. The default log sink remains an
  application-owned writer; file sinks now provide [automatic rotation](logging.md).
- The live S3/R2 baseline is recorded in milestone 11. Milestone 24 retained it
  after comparing all adapter source; only shared disk error classification
  changed and passed fresh native/race tests. This is not a later live-account run. Optional real-account email sends
  remain unverified; prior acceptance used controlled provider/native SMTP tests.
  Do not infer permission to send external email from the production audit.
- Controlled native cold/incremental build, generation, memory, code size and editor
  measurements, independent packaging and dependency/security review passed. See
  [recorded measurements](developer-resource-measurements.md). Historical
  constrained-VM results remain separate evidence. Milestone 25 remains deferred.

The full gate and the requested separate complete-framework audit/fix round
passed. This reconciliation records their scope; it does not replace repeatable
checks for future changes.
