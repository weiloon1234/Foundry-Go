# Milestone 3 model-files recipe sources

2026-11-07. Worktree `Foundry-Go-wt-m3-model-files`, branch
`lane/m3-model-files`. Implementation and source review only. No tests, builds,
vet, documentation checks, generation checks, or browser checks were run.
`gofmt` formatted the changed Go sources.

Rendering is pending. The authorized `go run ./internal/cmd/renderdocs` invocation
with the 15 changed guides failed before rendering because the sandbox denied
access to `/Users/weiloon/Library/Caches/go-build`. Following `COMMON.md`, rendering
is left to the lead. New directives currently have empty fences. Existing pilot
renderings remain intact. Source regions below are intended compilation inputs,
not a claim that this revision has compiled or passed acceptance.

## Inputs and ownership

Read the shared `step6/m3/COMMON.md`, proposal sections 1–3 and milestone 3,
owner decisions, the pilot's recipe ownership and approved normalization, and
[compiled-example authoring](../guides/developer-tooling-and-testing.md#author-compiled-guide-examples).

`S` below means `/Users/weiloon/Projects/rust-go/Foundry-Go-Starter/.claude/skills`.
A source such as `foundry-files:40–60` names lines of `S/foundry-files/SKILL.md`.
Read all six skills, including their `this-app` regions. Also inspected the ooo
model skill's mutator/hook/trigram consumers and Boss Club's typed-query/SQL-policy
text identified by the proposal. App source is provenance, not API authority;
framework declarations and this worktree's consumers determine the calls.

Fixture paths below are relative to `tests/fixtures/`. A `#name` is the literal
`doc-example` region. Each directive has one owning guide inclusion. Links to an
existing pilot owner retain its region rather than copying its call sequence.

## foundry-model

The declaration and basic query pilot is retained. `model-declarations.md` and
`model-queries.md` need no edits for this lane. The following table includes the
inherited coverage so the rest of the skill does not disappear from the inventory.

| Skill lines and recipe | Guide owner | Fixture region or retained reference |
| --- | --- | --- |
| 29–44: concrete fields, table binding, timestamps, soft deletion | [Declare tables](../guides/model-declarations.md#recipe-declare-table), [soft deletion](../guides/model-declarations.md#recipe-soft-delete) | `goway/migration/examples_test.go#roles-after`, `#force-after`; pilot retained |
| 50–60, 186–190: stored identity and complete table registration | [Register tables](../guides/model-declarations.md#recipe-register-tables) | `goway/migration/examples_test.go#application-after`, `#identity-after`; pilot retained |
| 63–67: scoped reads, optional and required keyed terminals | [Read models](../guides/model-queries.md#recipe-read-models) | `goway/migration/examples_test.go#reads-after`; pilot retained |
| 68–71, 82–89: locks and writes in one transaction | [Lock models](../guides/row-locking.md#recipe-lock-models), [waiting](../guides/row-locking.md#recipe-lock-waiting) | `consumer/advancedqueries/locking_recipes_test.go#locked-write`, `#skip-locked` |
| 68–71: joined lock targets and per-table policies | [Lock joins](../guides/row-locking.md#recipe-lock-joins), [lock policies](../guides/row-locking.md#recipe-lock-policies) | Same file: `#lock-joined-record`, `#independent-lock-policies` |
| 82–89, 108–113: transaction-required composition | [Locked CTE](../guides/transaction-query-composition.md#recipe-locked-cte) | Existing `consumer/transactionqueries/models.go#transaction-query-composition` |
| 72–81, 85–96: writes, assignments, presence and unique-column locking | [Write models](../guides/model-writes.md#recipe-write-models), [lock before a unique-column change](../guides/model-writes.md#recipe-lock-write) | Pilot `goway/migration/lifecycle_test.go#insert-member`; `goway/migration/examples_test.go#writes-after`, `#unique-column-lock` |
| 99–107, 114–116: predicates, expression adapters, row schemas and mappings | [Filter expressions](../guides/model-queries.md#recipe-filter-expressions), [declare rows](../guides/model-projections.md#recipe-declare-rows), [select rows](../guides/model-projections.md#recipe-select-rows) | Pilot `consumer/scalarqueries/text_row_test.go#computed-text`, `consumer/reports/rows.go#user-summary`, `consumer/reports/guide.go#paginate-projection` |
| 108–113: grouped rows, scalar reads and scalar subquery mappings | [Grouped and scalar rows](../guides/model-projections.md#recipe-grouped-rows) | `consumer/advancedqueries/subqueries_postgres_test.go#model-projections`, `#read-scalar`, `#scalar-projection` |
| 117–120: eager loading and existing parents | [Load relations](../guides/model-relations.md#recipe-load-relations) | Pilot `goway/migration/examples_test.go#relations-after` |
| 124–130: trigram candidates, scores and threshold in one transaction | [Trigram search](../guides/trigram-search.md#recipe-trigram-search) | Pilot `consumer/trigramqueries/search.go#find-duplicates`, `#similar-reviews` |
| 134–153, 180–185: explicit accessors and mutation input | [Access fields](../guides/model-accessors.md#recipe-access-field), [mutate fields](../guides/model-mutators.md#recipe-mutate-field) | Pilot `goway/migration/examples_test.go#accessors-after`, `#inputs-after`; existing owner prose retains omission, NULL, purity and normalization rules |
| 45–49, 154–164: hooks and typed audit disclosures | [Write hooks](../guides/model-hooks.md#recipe-write-hooks), [audit](../guides/audit.md#recipe-register-audit) | Pilot `goway/migration/examples_test.go#hooks-after`, `#write-options`, `#audit-registration` |

The pilot explicitly leaves constructor families, enum annotations, schema types,
scaffolding and advanced families as reference reading. Preserve that boundary:
lines 26–34 route to [typed values](../guides/typed-values.md) and
[model scaffolding](../guides/model-scaffolding.md); lines 121–123 retain the
UUIDv7/default explanation at [model writes](../guides/model-writes.md#recipe-write-models)
and explicit `model.NewIDAt` at [typed values](../guides/typed-values.md).
Correlated scalar adapters remain at [correlated queries](../guides/model-correlations.md),
which already includes `consumer/correlations/correlations_postgres_test.go#model-correlations`.
No new coverage is claimed for those reference sections.

## foundry-model-extensions

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 16–33, 37–48: slot kinds, exact field binding, one owner and stored identity | [Declare slots](../guides/model-extension-slots.md#recipe-declare-slots), [stored names](../guides/model-extension-slots.md#recipe-stored-slot-identity) | Existing `goway/models/extensions/models.go#model-extension-slots-article`, `#model-extension-slots-owner` |
| 41–52, 101–109: policy handles, concrete metadata codec, From and All | [Slot policies](../guides/model-extension-slots.md#recipe-slot-policies) | Existing `goway/guideadapters/slots_test.go#model-extension-slots-article-extensions`; `consumer/articles/tables.go#bind-slot-managers`, `#slot-relations` |
| 53–59, 110–112: declaration parts, Models, WithTables and construction-time binding | [Register slots](../guides/model-extension-slots.md#recipe-register-slots) | Existing `goway/guideadapters/slots_test.go#model-extension-slots-article-declaration`; `consumer/articles/slot_recipes.go#configure-slots` |
| 59–65: translation synchronization, metadata save/removal in the caller transaction | [Write slots](../guides/model-extension-slots.md#recipe-write-slots) | Existing `goway/guideadapters/slots_test.go#model-extension-slots-save-article`; `consumer/articles/http.go#write-slot-values` |
| 62–65: prepare, publish and clear files | [Publish files](../guides/attachments.md#recipe-publish-files), [retire files](../guides/attachments.md#recipe-retire-files) | `consumer/articles/attachment_recipes.go#publish-slot-file`, `#clear-slot-files`; slot guide links to this owner |
| 66–69: eager slot loading and existing-parent budgets | [Load slots](../guides/model-extension-slots.md#recipe-load-slots) | Existing `goway/guideadapters/slots_test.go#model-extension-slots-articles`; existing-parent terminal owner remains model-relations |
| 70–75: Rule, MergeRule, concrete metadata validation and file Accepts | [Validate slots](../guides/model-extension-slots.md#recipe-validate-slots) | Existing `consumer/articles/http.go#article-input-rules`; new `#slot-upload-rules` in the same file |
| 76–80, 112–114: loaded-state checks, editor entries and localized display | [Read slots](../guides/model-extension-slots.md#recipe-read-slots) | `consumer/articles/slot_recipes.go#loaded-slot-values`, `#resolve-slot-text` |
| 84–89, 106–109: stored names and strict metadata evolution | [Stored names](../guides/model-extension-slots.md#recipe-stored-slot-identity), [slot policies](../guides/model-extension-slots.md#recipe-slot-policies) | Owner prose beside the same declaration regions; no executable fence |
| 90–94: locale-keyed client values and cleanup/audit exclusions | [Wire values](../guides/client-contracts.md#recipe-wire-values), [slot cleanup](../guides/model-extension-slots.md#recipe-slot-cleanup), [audit](../guides/audit.md#recipe-register-audit) | Existing pilot `consumer/clientcontracts/testdata/guide-recipes.ts#locale-fields`; cleanup and audit contracts remain at their existing owners |

App namespaces, filenames, selected capabilities and stored domain names stay in
the app. No app layout or registry was changed. No unresolved in-lane recipe.

## foundry-migration

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 21–29: model declarations do not create schema; table registration is separate | [Declare tables](../guides/model-declarations.md#recipe-declare-table), [register tables](../guides/model-declarations.md#recipe-register-tables) | Pilot declaration and registration regions retained |
| 33–42, 70–76: immutable keys, dependencies, checksums and registry definitions | [Declare migrations](../guides/migrations-and-seeding.md#recipe-declare-migration) | `consumer/migrations/migrations.go#migration-definitions` |
| 43–46, 58–65, 75–76, 85–89: configured targets and selected-connection command runner | [Migration targets](../guides/migrations-and-seeding.md#recipe-migration-targets) | `consumer/database_command_test.go#application-migrations` |
| 40–46, 58–65: inspect history and explicitly apply | [Run migrations](../guides/migrations-and-seeding.md#recipe-run-migrations) | `consumer/articles/migration_recipes.go#inspect-migrations`, `#apply-migrations` |
| 47–48: nontransactional indexes and explicit reconciliation | [Online migrations](../guides/migrations-and-seeding.md#recipe-online-migration) | `consumer/security/contracts.go#online-migrations`, `#reconcile-migration` |
| 50–56, 81–84: transactional, repeatable seeding | [Seed data](../guides/migrations-and-seeding.md#recipe-seed-data) | `consumer/articles/seeding.go#seed-reference-data`; stable supplied key and DO NOTHING preserve existing values |

Scaffold CLI syntax remains in the existing development-scaffolding section;
app launcher names, counter allocation, origin strings, checksum-pin workflow,
account policy and database-target authorization are not framework recipes.
No migrations or seeders were executed. No unresolved in-lane recipe.

## foundry-files

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 15–21, 85–89: owner reuse, slot/collection policy, cardinality and disk | [Declare collections](../guides/attachments.md#recipe-declare-collection), [slot policies](../guides/model-extension-slots.md#recipe-slot-policies) | Existing `consumer/profiles/profile.go#attachments-avatar`, `#attachments-example-2`; slot regions above |
| 22–29, 95–100: correlated file kinds and bounded image inspection | [File kinds](../guides/http-uploads.md#recipe-file-kinds), [image kinds](../guides/http-uploads.md#recipe-image-kinds), [separate messages](../guides/http-uploads.md#recipe-image-messages) | `consumer/profiles/upload_rules.go#document-kinds`, `#image-kinds`, `#image-inspection-messages` |
| 30–32, 96–100: configured image engine and explicit processing plan | [Process images](../guides/imaging.md#recipe-process-image) | `consumer/profiles/image_recipes.go#configured-image-engine`, `#process-image` |
| 33–39: collection Replace/Add and bound FileSource publication | [Publish collection/file sources](../guides/attachments.md#recipe-declare-collection) | Existing `consumer/profiles/profile.go#attachments-example-2`; `consumer/profiles/attachment_recipes.go#add-collection-file`; `consumer/articles/http.go#publish-file-sources`; `consumer/articles/attachment_recipes.go#append-slot-file` |
| 40–49: prepare before transaction, reader ownership, ReplaceIn/AddIn and unknown outcomes | [Publish files](../guides/attachments.md#recipe-publish-files) | `consumer/articles/attachment_recipes.go#publish-slot-file`; `consumer/profiles/attachment_recipes.go#prepare-collection-file`, `#publish-collection-file` |
| 50–53: DetachIn/ClearIn, owner locks and post-commit cleanup | [Retire files](../guides/attachments.md#recipe-retire-files) | `consumer/profiles/attachment_recipes.go#retire-collection-files`; `consumer/articles/attachment_recipes.go#clear-slot-files` |
| 54–60: transaction-current FirstIn/ListIn and collection adapters | [Read files](../guides/attachments.md#recipe-read-files) | `consumer/articles/attachment_recipes.go#transaction-current-files` |
| 61–67, 92–94: bounded reconciliation and cleanup grace | [Publish/reconcile files](../guides/attachments.md#recipe-publish-files) | `consumer/articles/attachment_recipes.go#reconcile-files`; existing prose owns tombstones, unknown keys and grace |
| 68–72, 89–90: deferred authorization, downloads and read-error mapping | [Storage HTTP](../guides/storage.md#recipe-storage-http) | `consumer/storing/guide.go#deferred-storage-errors`; `consumer/storing/files.go#download-document` |
| 73–74, 101–103: disk operations and managed local store | [Store bytes](../guides/storage.md#recipe-store-files), [local disk](../guides/storage.md#recipe-local-disk) | `consumer/storing/guide.go#store-and-open`; `consumer/storing/files.go#local-disk` |
| 89–90: multipart contracts and persistence before request cleanup | [Multipart input](../guides/http-uploads.md#recipe-multipart-input), [endpoint](../guides/http-uploads.md#recipe-multipart-endpoint), [storage HTTP](../guides/storage.md#recipe-storage-http) | `consumer/httpuploads/forms.go#multipart-input`, `#multipart-endpoint`; `consumer/storing/files.go#persist-upload` |

The upload guide links typed message declaration/translation to its existing
[validation-message owner](../guides/validation-messages.md), rather than retaining
an unbound generic `ImageArgsMessage` fragment. Allowed formats, byte/dimension
limits, image backend deployment choices and schedule frequency remain app policy.
The processing recipe retains the imaging guide's format/backend reference
sections without claiming that their advanced fences were converted.

## foundry-async

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 14–17, 76–83: process-local AfterCommit and rollback | [After-commit events](../guides/events.md#recipe-after-commit-event), [realtime after commit](../guides/websocket.md#recipe-publish-channel) | `consumer/eventqueries/events.go#observer-after-commit`; `consumer/realtime/configured.go#publish-after-commit` |
| 18–25: injected typed producer and durable enqueue in the business transaction | [Enqueue events](../guides/outbox.md#recipe-enqueue-event), [enqueue jobs](../guides/jobs.md#recipe-enqueue-job) | `consumer/eventqueries/durable.go#register-event-outbox`; `consumer/eventqueries/guide.go#enqueue-event`; `consumer/background/guide.go#enqueue-job` |
| 22–25, 30–32: configured EventOutbox and framework-owned envelope/migrations | [Durable events](../guides/events.md#recipe-durable-events) | `consumer/eventqueries/configured_outbox.go#event-outbox`; `consumer/eventqueries/guide.go#configured-event-producer` |
| 26–29: queued listener and distinct delivery identity | [Queued listener](../guides/events.md#recipe-queued-listener) | `consumer/eventqueries/configured_outbox_postgres_test.go#queued-listener`; durable-event owner retains OutboxID semantics |
| 36–42: typed job payload, stable definition, configured handler and dispatch | [Declare jobs](../guides/jobs.md#recipe-declare-job) | `consumer/background/welcome.go#job-payload`, `#job-definition`, `#dispatch-job`; `consumer/background/guide.go#configured-job` |
| 43–44: application Calendar, job target, overlap and timeout | [Schedule jobs](../guides/scheduler.md#recipe-schedule-job), [explicit-zone schedule](../guides/scheduler.md#recipe-declare-schedule) | `consumer/background/guide.go#calendar-job`; `consumer/scheduling/reports.go#daily-schedule` |
| Durable message inspection supporting 18–29 | [Reload event](../guides/outbox.md#recipe-reload-event) | `consumer/eventqueries/guide.go#reload-event` |

Mail, notifications, outbound HTTP/webhooks and operations have owners outside
this lane. Their unclaimed extraction work is listed below. App payload choices,
locale policy, delivery profile, schedule frequency and retry authorization stay
in the app.

## foundry-realtime

| Skill lines and recipe | Guide owner | Fixture region |
| --- | --- | --- |
| 16–18, 36–42, 80–84: nominal channels, typed events and shared declaration identity | [Declare channels](../guides/websocket.md#recipe-declare-channel), [authorize rooms](../guides/websocket.md#recipe-authorize-rooms) | `consumer/realtime/orders.go#channel-events`, `#register-channel`, `#owned-rooms`; `consumer/realtime/configured.go#owned-channel`, `#private-resource-channel` |
| 16–29, 73–79, 91–94: shared HTTP hub, actor authentication and typed ticket redeemers | [Realtime tickets](../guides/websocket.md#recipe-realtime-tickets) | `consumer/realtime/configured.go#realtime-tickets`; token-family/refresh contract retained in owner prose |
| 43–45, 60–65: publish after commit and delivery limits | [Publish channels](../guides/websocket.md#recipe-publish-channel) | `consumer/realtime/orders.go#publish-room`; `consumer/realtime/configured.go#publish-after-commit`; required durable delivery links to outbox |
| 49–56, 86–90: configured publisher-only process and retained publisher dependency | [Realtime publisher](../guides/websocket-distributed.md#recipe-realtime-publisher) | `consumer/realtime/configured.go#configured-realtime-publisher` |
| 49–56: direct distributed hub/publisher composition | [Distributed hub](../guides/websocket-distributed.md#recipe-distributed-hub) | `consumer/realtime/orders.go#distributed-hub`, `#standalone-publisher` |
| 63–65: bounded frames, presence DTO disclosure, replay and ephemeral fan-out | [Declare channels](../guides/websocket.md#recipe-declare-channel), [distributed hub](../guides/websocket-distributed.md#recipe-distributed-hub) | Channel replay declaration above; existing bounds, presence and pub/sub reference prose retained |

Browser TypeScript, ticket issuance and client export remain with their other
owners, listed below. No TS source was added, so no compiler-input list changed.

## Unresolved owner work, not claimed by this lane

These are framework recipes mentioned by the six skills, not app policy. They
must stay visible when the lead combines lanes. This work does not authorize
removing their app teaching or claim new source/anchor coverage for them.

| Skill lines | Existing owner outside this lane | Remaining extraction |
| --- | --- | --- |
| foundry-files 22–29, 95–100 | [Validation messages](../guides/validation-messages.md) | Typed WithTranslation arguments; the upload guide now links here. Check the validation lane's final anchor and source region. |
| foundry-async 45–48, 84 | [Email](../guides/email.md), [notifications](../guides/notifications.md), [localization](../guides/localization.md) | Configured/localized delivery, AttachStored/EmailAttachment/AttachUpload and captured-locale handler calls, including this-app teaching. |
| foundry-async 49–52 | [HTTP client](../guides/http-client.md), [webhooks](../guides/webhooks.md) | Named client/deadline, original-body signature verification and authenticated deduplication. |
| foundry-async 56–64 | [Production operations](../guides/production-operations.md) | PruneService in FeatureDeclarations.Pruning, construction-time resolution, worker maintenance and scheduler catch-up. |
| foundry-realtime 19–22, 30–32, 46–47 | [Client contracts](../guides/client-contracts.md), [tokens](../guides/tokens.md) | Browser realtimeProtocols(ticket), fresh tickets, handle/socket disposal and generated client export. Keep issuance with the token/HTTP owner. |

No check/renderer/framework changes were made or requested. Rendering and all
verification remain lead work; in-lane source mappings above are ready for that
step, with no compiler or runtime result asserted.

## Marker-only exceptions outside the named fixture packages

New functions stay in articles, advancedqueries, background, eventqueries,
realtime, profiles (attachments/imaging) and storing (storage). Existing examples
outside those packages receive markers and gofmt whitespace only:

- `consumer/migrations/migrations.go`: the historical definition/dependency owner.
- `consumer/security/contracts.go`: the existing online migration/reconciliation owner.
- `consumer/database_command_test.go`: the configured application database-command example.
- `consumer/scheduling/reports.go`: the existing explicit-timezone schedule owner.
- `consumer/httpuploads/forms.go`: the existing generated multipart declaration and endpoint.

No owned package already held these declarations. Reusing their selected source
avoids duplicating migration history, multipart contracts or schedule setup.
The transaction CTE guide reuses its existing `transactionqueries/models.go`
region without modifying that package. Existing goway/pilot regions are unchanged.

## Lead commands

Run rendering first, in the normal writable Go-cache environment:

```sh
go run ./internal/cmd/renderdocs --guide attachments.md --guide events.md --guide http-uploads.md --guide imaging.md --guide jobs.md --guide migrations-and-seeding.md --guide model-extension-slots.md --guide model-projections.md --guide outbox.md --guide row-locking.md --guide scheduler.md --guide storage.md --guide transaction-query-composition.md --guide websocket-distributed.md --guide websocket.md
make docs-check
```

At the lead's combined checkpoint, `make fixture-check` compiles/vets/tests the
selected consumer modules and passes their actual Go input inventory to checkdocs.
Use that existing target once for the combined milestone; this lane did not run
it. Existing PostgreSQL/Redis integration and TypeScript gates remain the lead's
milestone decision. No declaration generation is needed for these handwritten
functions and marker-only edits; no DTO, enum, manifest or generated file changed.

## Changed files

- This evidence file.
- [docs/guides/attachments.md](../../docs/guides/attachments.md)
- [docs/guides/events.md](../../docs/guides/events.md)
- [docs/guides/http-uploads.md](../../docs/guides/http-uploads.md)
- [docs/guides/imaging.md](../../docs/guides/imaging.md)
- [docs/guides/jobs.md](../../docs/guides/jobs.md)
- [docs/guides/migrations-and-seeding.md](../../docs/guides/migrations-and-seeding.md)
- [docs/guides/model-extension-slots.md](../../docs/guides/model-extension-slots.md)
- [docs/guides/model-projections.md](../../docs/guides/model-projections.md)
- [docs/guides/outbox.md](../../docs/guides/outbox.md)
- [docs/guides/row-locking.md](../../docs/guides/row-locking.md)
- [docs/guides/scheduler.md](../../docs/guides/scheduler.md)
- [docs/guides/storage.md](../../docs/guides/storage.md)
- [docs/guides/transaction-query-composition.md](../../docs/guides/transaction-query-composition.md)
- [docs/guides/websocket-distributed.md](../../docs/guides/websocket-distributed.md)
- [docs/guides/websocket.md](../../docs/guides/websocket.md)
- [tests/fixtures/consumer/advancedqueries/locking_recipes_test.go](../../tests/fixtures/consumer/advancedqueries/locking_recipes_test.go)
- [tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go](../../tests/fixtures/consumer/advancedqueries/subqueries_postgres_test.go)
- [tests/fixtures/consumer/articles/attachment_recipes.go](../../tests/fixtures/consumer/articles/attachment_recipes.go)
- [tests/fixtures/consumer/articles/http.go](../../tests/fixtures/consumer/articles/http.go)
- [tests/fixtures/consumer/articles/migration_recipes.go](../../tests/fixtures/consumer/articles/migration_recipes.go)
- [tests/fixtures/consumer/articles/seeding.go](../../tests/fixtures/consumer/articles/seeding.go)
- [tests/fixtures/consumer/articles/slot_recipes.go](../../tests/fixtures/consumer/articles/slot_recipes.go)
- [tests/fixtures/consumer/articles/tables.go](../../tests/fixtures/consumer/articles/tables.go)
- [tests/fixtures/consumer/background/guide.go](../../tests/fixtures/consumer/background/guide.go)
- [tests/fixtures/consumer/background/welcome.go](../../tests/fixtures/consumer/background/welcome.go)
- [tests/fixtures/consumer/database_command_test.go](../../tests/fixtures/consumer/database_command_test.go)
- [tests/fixtures/consumer/eventqueries/configured_outbox.go](../../tests/fixtures/consumer/eventqueries/configured_outbox.go)
- [tests/fixtures/consumer/eventqueries/configured_outbox_postgres_test.go](../../tests/fixtures/consumer/eventqueries/configured_outbox_postgres_test.go)
- [tests/fixtures/consumer/eventqueries/durable.go](../../tests/fixtures/consumer/eventqueries/durable.go)
- [tests/fixtures/consumer/eventqueries/events.go](../../tests/fixtures/consumer/eventqueries/events.go)
- [tests/fixtures/consumer/eventqueries/guide.go](../../tests/fixtures/consumer/eventqueries/guide.go)
- [tests/fixtures/consumer/httpuploads/forms.go](../../tests/fixtures/consumer/httpuploads/forms.go)
- [tests/fixtures/consumer/migrations/migrations.go](../../tests/fixtures/consumer/migrations/migrations.go)
- [tests/fixtures/consumer/profiles/attachment_recipes.go](../../tests/fixtures/consumer/profiles/attachment_recipes.go)
- [tests/fixtures/consumer/profiles/image_recipes.go](../../tests/fixtures/consumer/profiles/image_recipes.go)
- [tests/fixtures/consumer/profiles/upload_rules.go](../../tests/fixtures/consumer/profiles/upload_rules.go)
- [tests/fixtures/consumer/realtime/configured.go](../../tests/fixtures/consumer/realtime/configured.go)
- [tests/fixtures/consumer/realtime/orders.go](../../tests/fixtures/consumer/realtime/orders.go)
- [tests/fixtures/consumer/scheduling/reports.go](../../tests/fixtures/consumer/scheduling/reports.go)
- [tests/fixtures/consumer/security/contracts.go](../../tests/fixtures/consumer/security/contracts.go)
- [tests/fixtures/consumer/storing/files.go](../../tests/fixtures/consumer/storing/files.go)
- [tests/fixtures/consumer/storing/guide.go](../../tests/fixtures/consumer/storing/guide.go)
