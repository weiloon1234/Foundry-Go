# Stage 9 lane B cleanup-barrier audit

Read-only source audit, 2026-10-25. Base: `688859599b43888ab57dc5bbbfcc200289211488`, plus the accepted, uncommitted B read-completion delta. Only this report is written. No source, generated output, manifest, catalogue or contract changes; no build, tests, services or generation run. Results describe inspected source, not a verified integrated build.

## Authority and removal rule

Read `proposals/foundry-go-goway-data-layer/patches/stage8c-plan.md` §5 and adjacent `stage8c-decisions.md`. **The decisions supersede Stage 9 decision 5:** close callers; run and freeze legacy SQL/bindings/hydration expectations and footprint evidence on the integrated tree with commit, hashes and commands; switch parity consumers to frozen records; retire the model/projection emitters and old oracles in 8c. Runtime `database/query` retirement is separate (8d).

“Remove” below means the barrier change after its stated prerequisites, not deletion during this audit. B removes handwritten coexistence factories/directives; the lead publishes generated-output/manifests cleanup through the Makefile. Keep model structs, model/key/input/storage types, relation slots, native ORM/rows declarations, mutators/accessors, DTO/enum/config generation and contract bytes. Preserve assertions, including failure, callback, lock, cancellation and reconciliation behavior.

## Scope and method

41 positive packages: B's 14, reassigned D2's seven, D3's 19, and clientcontracts. The census finds **38 remaining directives: 35 models and three projections, in 16 packages**. All 38 corresponding model/projection generated files are removal candidates, subject to the dependencies below. Other generated files are not implied removal candidates.

Searched handwritten Go throughout the worktree (including external tests, negative testdata and code strings), generated declarations and generated model receiver methods. Matched imported aliases and same-package generated symbols, then inspected factories and implicit `model.Identifiable` requirements. The explicit-reference inventory below excludes generated-to-generated implementation details and comment-only references. Managed field comments are addressed separately. Owned package Markdown files contain no remaining references to their generated model/projection symbols.

Also inspected all 22 requested saved patch files. Reconstructed 2,342 non-generated Go file postimages **in memory** from each patch's available `index` preimage blob plus hunks, without applying patches or changing git state; no missing blobs. Searched those postimages for the same references. This distinguishes additions/context from removed callers and does not treat historical/cumulative patches as all pending or authoritative over the newer worktree. Patch fingerprints appear below.

## Removal decisions and dependency owners

| Item | Barrier action / owner |
| --- | --- |
| Unused hook/relation/scope/audit factories and legacy-only instrumentation | B can remove together with directives/output publication, retaining the native replacement and shared behavior helpers. Exact package inventory follows. |
| `linkqueries` old SQL parity and creation-adapter oracle | B + lead: freeze old behavior before removing the old side, `runLinks`/`endpoints`, old observer-registration branch and the application-implementable legacy creation adapter. Keep native tests and the sealed creation contract; do not port the untrusted bridge into ORM. |
| `observerqueries/orm_test.go:35` mixed old/new observer-family rejection | B + lead: preserve/freeze this compatibility assertion before deleting the old observer construction. Keep native registration-count, factory-laziness, instance isolation and ordering assertions. This is an oracle dependency, not ordinary dual registration. |
| `cursorqueries/legacy_clauses_postgres_test.go:13`; `pagequeries/legacy_clauses_postgres_test.go:13` | **Lane C** must publish the `rows.RecordRead` DISTINCT ON adapter. B then ports the two complete-record cursor/page assertions. Neither may be deleted just to close the caller barrier. |
| `httpmodels/bindings_postgres_test.go:141–180` | **Lane A + lead integration**: saved `laneA-full2-on-fdaff938.patch` removes the generated soft/retrieval callers in favor of `Groups`, `Members`, `MemberLinks`, `orm.Keyed`. After integration these no longer block B's declarations. |
| `httppagination/pagination.go:70–73` | **Lane D + lead integration**: still returns `mutatorqueries.MemberQuery` and calls `QueryMutatorMembers`/`MemberFields`. No cutover of this file exists in the requested saved patch set. User reports it accepted; integrate that accepted artifact before retiring `mutatorqueries` output. Do not infer it is present from D's older full patches. |
| `testdata/compilefail/outbox_field_id/invalid.go:8` | **B**: port `EventLinkDraft{}.SetMessageID(outbox.ID[string]{})` to the typed `eventqueries.Links.MessageID.Set` negative, retaining the outbox payload mismatch. No replacement found in requested patches. Undefined draft names after cleanup would be a false-positive negative. |
| `recovering.Member` generated `FoundryIdentity` | **B**, before generated-file removal: preserve a handwritten identity method backed by the published `MemberIdentity`, or obtain an explicitly approved shared API change. Current ORM lookups do not remove the `model.Identifiable` constraint. |
| `nestedbindings.Team` generated `FoundryIdentity` | **B**, same requirement for native `TeamRecords` identity; the PostgreSQL auth-binding assertion still instantiates `auth.DefineProvider[Team]`. |
| `models.User` generated `FoundryIdentity`, used by B service consumers | **Lane C** owns `models` and must preserve its handwritten identity contract at cleanup (coordinate with **Lane D**, identity/auth framework owner). Authenticating, notifying and realtime are already query-migrated but remain implicit method callers. No replacement method found in current source or the requested patch postimages. |
| Saved D nestedbindings oracle | `laneD-full-on-79db1cf8.patch` postimage `nestedbindings/tables_test.go:14` adds `QueryProjects`/`ProjectFields` parity, unlike current B's native-only file. **Lead + B/D** must reconcile the overlapping historical patch; freeze that assertion if retained, and do not reintroduce its generated callers after cleanup. |
| Generator recognition of native persistence models | **Lane A + lead**, Stage 8c decision 3: native `orm.Define`/table recognition must replace annotation/generated-model discovery. Preserve DTO field/embedding/map-key rejection when removing any of these directives; `FoundryIdentity` alone is insufficient. |

## Package-by-package inventory

Paths below are relative to `tests/fixtures/consumer/`. “Generated-only” means its direct factory caller is the corresponding generated model file (listed later), not that the handwritten file is safe to delete wholesale.

| Package | Remaining coexistence and callers | Disposition / retained native owner |
| --- | --- | --- |
| auditqueries | `domain.go:19 Domain` uses `AccountAuditing`, `AccountAuditPolicy`, `LabelAuditing`, `LabelAuditPolicy`; no handwritten caller. `accountHooks` at :26 is generated-only and consumes `AccountChanges`. `history.go:25 EmailAfter` uses `AccountChanges.Audit` and `AccountAuditFields`; no handwritten caller. Two model directives. | Remove old `Domain`, `accountHooks`, `EmailAfter`; retain `ORMDomain`, `ormAccountHooks`, `ORMEmailAfter` (`orm_audit.go`), `Approve`, pools, history key and shared errors. |
| binarymodels | Record model and View projection directives; no remaining handwritten generated-symbol caller or coexistence factory. | Remove directives/output at barrier. Keep `Records`, `Views`, Input/Payload types and `MutateBody`/`MutateNote`/`MutateEncoded`; preserve byte ownership, nullable and input/storage assertions. |
| eventqueries | EventLink model directive; external `outbox_field_id` negative still constructs `EventLinkDraft` (above). Ordinary event integration uses native declarations/observers. | B must port the negative, then remove directive/output. Keep event/outbox assertions and native `Links`. |
| hookqueries | `member.go:48 Trace.Changes`, :56 `Trace.AfterCommit`, :64 `record`, :92 `memberHooks`; old hooks reference `MemberDraft`, `MemberChanges`, `MemberFields`, `QueryHookMembers` including reentrant/create and introducer lookup. Only old factory/record consumes old Trace fields; generated model calls the factory. Member/Log directives. | Remove legacy factory/helper and legacy fields; keep `recordORM`, `ormMemberHooks`, `ORMChanges`, `ORMAfterCommit`, shared trace settings and mutators. No current test uses old Trace fields. |
| inputqueries | `hooks.go:16 State.Captured []MemberChanges`; :27 `memberHooks` uses MemberHooks/MemberDraft/MemberChanges. Generated-only; old factory alone writes Captured. Member directive. | Remove old factory/Captured; retain `ORMCaptured`, `BeforeInput`, native Inspect/veto/context helpers, input/storage/accessor contracts and ORM hooks in `orm.go`. |
| linkqueries | `models.go:80 defineMemberRelations`; :90 `memberReads`; :93 `groupReads`; `insert_models.go:42 archiveHooks`, :51 `archiveReads`; `hooks.go:19 Trace.Changes`, :41 `membershipHooks`, :43 exported `Hooks`; seven model directives. `fixture_test.go:24 linkDatabaseWith` has a legacy observer branch (:34–35), reached via `linkDatabase`/`runLinks` solely by `legacy_creation_bridge_postgres_test.go:15`; `endpoints` (:84) constructs old drafts. `orm_test.go:36` runs old SQL parity. | Freeze the two oracle paths, then remove old adapters/factories/Trace.Changes and legacy fixture branch. Keep ORM registration, `membershipOperation`, shared trace/read instrumentation, native table/relation/hook/read factories and all new-API assertions. `unusedMembershipCreation.FoundryCreateMutation` at legacy test :45 retires with its old test. |
| mutatorqueries | Member model directive; no same-package generated-symbol call. External `httppagination/pagination.go:70–73` still needs MemberQuery/QueryMutatorMembers/MemberFields. D2 imports only stored/input/getter/model-ID contracts. | Wait for D's pagination integration; then remove only model directive/output. Keep State enum generation, named mutators, explicit accessors and typed Input* declarations. |
| observerqueries | `models.go:46 Trace.Changes`; :91 `localHooks`; :94 `ObserverHooks` use RecordDraft/RecordChanges/RecordHooks. Factory is generated-only except `orm_test.go:35` deliberately constructs `NewRecordObserver("legacy")` for incompatible-family rejection. Record/Plain/Effect directives. | Freeze/retire only the legacy part of that test; remove old hook factory/helper/Trace.Changes. Keep ORMObserverHooks, native observer count/laziness/isolation assertions and one native family per running application. |
| retrievalqueries | `hooks.go:29 memberRetrieval` wraps generated `MemberRetrievalHooks` and `QueryRetrievalMembers().Count`; `models.go:53 defineMemberRelations` uses generated Fields/RelationSet. Four directives. External HTTP binding references listed above. | After A integration, remove wrappers/directives/output; retain shared `memberRetrieved`, native count/retrieval/relations and the accepted `rows.EachRead` complete-record streaming tests. `legacy_iteration_postgres_test.go` and the legacy fixture-registration selector are already removed in the accepted local delta. |
| softqueries | `hooks.go:17 Trace.Changes`, :31 `memberHooks`, :34 exported `Hooks` consume generated draft/changes/hooks; `models.go:59 defineMemberRelations`, :68 `defineMemberAggregates` consume Fields/Relations/sets. Four directives. HTTP binding still creates generated GroupDraft until A integration. | After A integration remove old factory/helper/Trace.Changes/relation/aggregate factories and directives/output. Retain native `ORMHooks`, declarations, soft token, per-model terminals, operations/committed instrumentation and visibility assertions. |
| tenantqueries | `models.go:51 TenantScope`, :59 `PublishedScope`, :62 `defineDocumentGlobalScopes`; :66 `defineDocumentRelations`, :80 `defineTagRelations`. Generated scopes/relations only; old scope variables consumed only by old scope source. Three directives. | Remove old scopes/source/relation factories; keep WithTenant/TenantFrom, native lazy/error-returning `documentScopes` in `orm_scopes.go`, typed relations and fail-closed tests. |
| timequeries | `hooks.go:17 State.Before MemberDraft`, :21 State.Changes, :32 `memberHooks` using old drafts/changes. Generated-only; old factory alone consumes Before/Changes. Member/Manual directives. | Remove old fields/factory/directives/output. Keep ORMCreatedAt/ORMUpdatedAt/ORMChanges, veto/committed state, managed timestamp carriers and Manual's explicit convention behavior. |
| upsertqueries | ExcludedRecord model directive only; no remaining generated query/draft/field use in handwritten tests. | Remove directive/output at barrier; all upsert/source/lookup assertions already use ORM. Stage 8c §5's older `*_postgres_test.go` claim is stale here. |
| vault | Entry model directive only; no projection directive, factory or handwritten generated-symbol use. | Remove directive/output at barrier; retain encrypted declarations/codec and redaction tests. |
| caching | No model/projection directive, coexistence factory or generated model API use. Native mutatorqueries getters/types in profile/cache callbacks. | Nothing to remove; retain cache behavior and assertions. |
| coordination | None; uses native caching and typed mutator model IDs. | Nothing to remove. |
| messaging | None; uses native caching and retained typed message contracts. | Nothing to remove. |
| limiting | None; typed mutator model IDs and limiter contracts. | Nothing to remove. |
| redisdata | None; native stored/getter projections plus model IDs; retained Activity/Profile enum/DTO codecs. | Nothing to remove; Redis tests stay opt-in, assertions unchanged. |
| rediscommands | None; typed ID/key families and native redisdata adapters. | Nothing to remove; retain raw Redis command assertions (not legacy SQL). |
| httpetags | None; ordinary member fields/accessor path and model-ID route. | Nothing to remove; keep cache validators and conditional HTTP assertions. |
| recovering | Member directive; `hooks.go:66 memberHooks` uses MemberHooks/MemberDraft/MemberChanges, called only by generated output. Native `ormMemberHooks` already uses WriteUnset. **Implicit generated identity dependency** in provider/recovery/credential workflows (below). | Remove old factory; retain `nextEmailRevision`, `checkEmailState`, native unset hook and all freshness/locking/redaction/reconciliation assertions. Supply handwritten identity before deleting model output. |
| authenticating | No local directive or generated top-level call; ORM provider/query/reference paths. **Implicit `models.User.FoundryIdentity`** via Provider/session/token/guard APIs. | C must preserve User identity; otherwise no B coexistence cleanup. Old §5 direct query claims are stale. |
| nestedbindings | Team/Project/Task directives; `models.go:42 defineTeamRelations`, :45 `defineProjectRelations`, :61 `projectRetrieval` (generated-only). Implicit Team identity in `bindings_postgres_test.go:190`. Saved D patch has an additional old parity caller. | Remove old relation/retrieval wrappers, keeping native records/links and shared `retrievedProject`; preserve Team identity; reconcile D patch. `nested_field_owner`/`nested_relation_owner` already reference native declarations in this worktree. |
| notifying | No model/projection directives or generated top-level calls. Native ORM inbox page and explicit references. **Implicit models.User identity** in recipient/binding/provider contracts. | C's identity cleanup prerequisite; keep DTO/notification generation and delivery/inbox assertions. |
| realtime | No model/projection directives or generated top-level calls. Native ORM reference passed to owned rooms/disconnect. **Implicit models.User identity** through OwnedRooms/auth contracts. | C's identity prerequisite; retain message DTO contracts and websocket assertions. |
| cursorqueries | Only old complete-record DISTINCT ON test remains, `legacy_clauses_postgres_test.go:13`; QueryUsers/QueryOrders/UserFieldsAt/OrderFieldsAt and old query join/cursor chain. | Wait for C RecordRead adapter, then B ports this assertion; all other clauses/terminals already migrated. |
| pagequeries | Only old complete-record DISTINCT ON test remains, `legacy_clauses_postgres_test.go:13`; same generated table/fields and old join/page chain. | Same C dependency; preserve complete hydrated model/page assertions. |
| genericdto | No model/projection directives or generated model API uses. | Retain generic DTO generation/contracts. |
| internal/authfixture | No generated model dependency; Actor's `FoundryIdentity` is handwritten (`authentication.go:23`). | Retain identity implementation and auth fixture. |
| localization | No model/projection directives or generated model API uses. | Retain localization/DTO/enum contracts. |
| mailing | No model/projection directives or generated model API uses. | Retain mail DTO/contracts/assertions. |
| modelvalidation | No generated model API use; native declarations in `tables.go` reference ordinary shared model structs. | Retain native database-validation rules and constraints; C retains model/key/enum types. |
| outgoing | No generated model API use or model/projection directives. | Retain event/job/DTO generation. |
| requestflow | No generated model API use or model/projection directives. | Retain DTO/input/validation contracts. |
| security | No generated model API use or model/projection directives. | Retain security/middleware assertions. |
| storing | No generated model API use or model/projection directives. | Retain storage/resource DTO assertions. |
| unions | No generated model API use or model/projection directives. | Retain union/DTO generation. |
| validationinput | No generated model API use or model/projection directives. | Retain validation DTO declarations. |
| validationrules | No generated model API use or model/projection directives. | Retain typed rule contracts. |
| clientcontracts | No model/projection directive or generated model query/projection use. Page/cursor forms already ORM. `contracts.go:40,43` Account reference/identity methods are handwritten. `contracts_test.go:197` “legacy” means a prior TypeScript manifest, not model-generator coexistence. | No B cleanup. Preserve every TypeScript/contract byte and all DTO generation; D owns still-legacy httppagination dependency, C owns reporting/shared models. |

## Implicit generated-method callers

Do not equate “no generated symbol text” with “safe to delete methods.” `model/reference.go:139` requires `FoundryIdentity`; `auth/provider.go:18,33` constrains Provider/DefineProvider to that interface and :82 calls the method. Passing an explicit ORM reference still leaves this requirement in place.

| Generated method owner | Actual callers / constraints | Needed before output deletion |
| --- | --- | --- |
| `recovering/member_foundry.gen.go:155` | `recovery.go:17–22` Provider/DefineProvider; reset/verification aliases :18–19 and constructors :36/:51; `credentials.go:17–20` sessions, tokens, login, revocations; `link_requests.go:25,28` challenge requests; their tests instantiate the same types. Shared constraints: `auth/password_model.go:18`, `auth/passwordreset/reset.go:42,53`, `auth/emailverification/verification.go:32,40`, `auth/session/sessions.go:19`, `auth/token/tokens.go:21`. | B preserves Member's identity behavior through the native identity declaration. Do not drop auth assertions or weaken the constraint. |
| `nestedbindings/team_foundry.gen.go:104` | `bindings_postgres_test.go:190` DefineProvider and :206 DefineGuard. | B supplies Team identity through TeamRecords. Project/Task have no found implicit identity caller in this scope. |
| `models/user_foundry.gen.go:160` | `authenticating/accounts.go:18–19,27`, session/token aliases/constructors; `notifying/orders.go:32–35,41–47,67` Recipient/Binding/Realtime/Inbox; `realtime/orders.go:49` OwnedRooms. Relevant shared constraints: `notifications/recipient.go:16,24`, `notifications/definition.go:37,63`, `notifications/inbox.go:35`, `notifications/realtime.go:32,40`. | C owns the shared model method; coordinate identity semantics with D. None of the specified patch postimages adds a handwritten replacement. |

All model receiver methods emitted for the 35 owned models were inspected: the remaining generated receiver surface is FoundryReference/FoundryIdentity. No other live implicit method dependency was identified for the seven D2 adapters; their plain stored values, IDs and handwritten Access* methods must remain.

## Exact remaining model/projection directives

Remove the directive, not the struct or unrelated annotation. The corresponding snake-case `<type>_foundry.gen.go` is generator-owned; the lead retires it and updates its manifest. Projection DTO conversion is unnecessary for these three projections: their concrete record structs already have rows declarations.

| Package | Directive / location |
| --- | --- |
| auditqueries | `Account` (model; `models.go:18`; `table=audited_accounts hooks=accountHooks`)<br>`Label` (model; `models.go:46`; `table=audited_labels primary=Code`) |
| binarymodels | `Record` (model; `record.go:15`; `table=binary_records primary=ID`)<br>`View` (projection; `record.go:38`) |
| eventqueries | `EventLink` (model; `event_link.go:9`; `table=event_links primary=ID`) |
| hookqueries | `Member` (model; `member.go:17`; `table=hook_members hooks=memberHooks`)<br>`Log` (model; `member.go:30`; `table=hook_logs`) |
| inputqueries | `Member` (model; `models.go:20`; `table=input_members hooks=memberHooks`) |
| linkqueries | `Member` (model; `models.go:17`; `table=link_members retrieval=memberReads`)<br>`Group` (model; `models.go:32`; `table=link_groups primary=Code retrieval=groupReads`)<br>`Membership` (model; `models.go:40`; `table=link_memberships hooks=membershipHooks`)<br>`NaturalLink` (model; `models.go:59`; `table=link_natural`)<br>`NullableLink` (model; `models.go:66`; `table=link_nullable`)<br>`Friendship` (model; `models.go:73`; `table=link_friendships`)<br>`Archive` (model; `insert_models.go:17`; `table=link_archives hooks=archiveHooks retrieval=archiveReads`) |
| mutatorqueries | `Member` (model; `member.go:17`; `table=mutator_members`) |
| observerqueries | `Record` (model; `models.go:18`; `table=observed_records hooks=localHooks`)<br>`Plain` (model; `models.go:25`; `table=observed_plain`)<br>`Effect` (model; `models.go:31`; `table=observed_effects`) |
| retrievalqueries | `Member` (model; `models.go:13`; `table=retrieval_members retrieval=memberRetrieval`)<br>`Group` (model; `models.go:34`; `table=retrieval_groups primary=Code`)<br>`Membership` (model; `models.go:40`; `table=retrieval_memberships`)<br>`MemberLabel` (projection; `models.go:47`) |
| softqueries | `Member` (model; `models.go:15`; `table=soft_members hooks=memberHooks`)<br>`Group` (model; `models.go:42`; `table=soft_groups primary=Code`)<br>`Membership` (model; `models.go:50`; `table=soft_memberships`)<br>`MemberLabel` (projection; `models.go:76`) |
| tenantqueries | `Document` (model; `models.go:32`; `table=tenant_documents`)<br>`Tag` (model; `models.go:73`; `table=tenant_tags primary=Code`)<br>`DocumentTag` (model; `models.go:85`; `table=tenant_document_tags`) |
| timequeries | `Member` (model; `models.go:13`; `table=time_members hooks=memberHooks`)<br>`Manual` (model; `models.go:24`; `table=time_manual timestamps=false`) |
| upsertqueries | `ExcludedRecord` (model; `models.go:5`; `table=excluded primary=ID`) |
| vault | `Entry` (model; `entry.go:14`; `table=vault_entries`) |
| recovering | `Member` (model; `member.go:11`; `table=recovery_members hooks=memberHooks`) |
| nestedbindings | `Team` (model; `models.go:16`; `table=teams`)<br>`Project` (model; `models.go:23`; `table=projects retrieval=projectRetrieval`)<br>`Task` (model; `models.go:35`; `table=tasks`) |

The generated managed-field documentation also remains in declaration files (for example `binarymodels/record.go:19–30`, `mutatorqueries/member.go`, `nestedbindings/models.go:30`). It names old Draft/Set/Clear APIs. Remove/update those **generator-owned comment blocks through the publication owner**, without deleting handwritten field/domain comments or changing contract documentation bytes inadvertently.

## Exact explicit generated-symbol use sites

Each row lists every remaining handwritten use site found in the owned positive packages, plus inbound callers of owned generated APIs. Paths are consumer-relative; `line: symbols` identifies the callers rather than listing every generated implementation. Method chains off those symbols (Draft setters/getters, Changes.Audit, query terminals) retire or port with that row. Generated model callers of handwritten factories follow the table.

| Caller file | Line: generated symbols |
| --- | --- |
| `auditqueries/domain.go` | `22: AccountAuditPolicy, AccountAuditing, LabelAuditPolicy, LabelAuditing`; `26: AccountHooks`; `27: AccountChanges, AccountHooks` |
| `auditqueries/history.go` | `25: AccountChanges`; `26: AccountAuditPolicy`; `30: AccountAuditFields` |
| `cursorqueries/legacy_clauses_postgres_test.go` | `15: QueryUsers`; `16: QueryOrders`; `17: OrderFieldsAt, UserFieldsAt`; `19: OrderFieldsAt, UserFieldsAt`; `22: UserFieldsAt` |
| `hookqueries/member.go` | `48: MemberChanges`; `56: MemberChanges`; `64: MemberChanges`; `92: MemberHooks`; `97: MemberHooks`; `98: MemberDraft`; `106: MemberDraft`; `111: MemberDraft, QueryHookMembers`; `118: MemberFields, QueryHookMembers`; `126: MemberDraft`; `140: MemberChanges`; `143: MemberChanges`; `146: MemberChanges`; `149: MemberChanges`; `155: MemberChanges` |
| `httpmodels/bindings_postgres_test.go` | `141: QuerySoftGroups`; `142: GroupDraft`; `168: QueryRetrievalMembers`; `169: MemberDraft`; `173: MemberDraft`; `180: MemberRelations` |
| `httppagination/pagination.go` | `70: MemberQuery`; `71: QueryMutatorMembers`; `73: MemberFields` |
| `inputqueries/hooks.go` | `16: MemberChanges`; `27: MemberHooks`; `28: MemberChanges`; `37: MemberHooks`; `38: MemberDraft`; `44: MemberDraft` |
| `linkqueries/fixture_test.go` | `34: MembershipHooks, NewMembershipObserver, RegisterMembershipObserver`; `35: MembershipHooks`; `86: MemberDraft, QueryLinkMembers`; `90: GroupDraft, QueryLinkGroups` |
| `linkqueries/hooks.go` | `19: MembershipChanges`; `41: MembershipHooks`; `43: MembershipHooks`; `45: MembershipChanges`; `51: MembershipHooks`; `52: MembershipDraft`; `58: MembershipDraft`; `62: MembershipChanges`; `63: MembershipChanges`; `64: MembershipChanges`; `65: MembershipChanges`; `66: MembershipChanges`; `69: MembershipChanges` |
| `linkqueries/insert_models.go` | `42: ArchiveHooks`; `43: ArchiveDraft, ArchiveHooks`; `51: ArchiveRetrievalHooks`; `52: ArchiveRetrievalHooks` |
| `linkqueries/legacy_creation_bridge_postgres_test.go` | `22: QueryLinkMemberships`; `23: MembershipFields`; `25: MembershipDraft` |
| `linkqueries/models.go` | `80: MemberRelationSet`; `81: FriendshipFields, GroupFields, MemberFields, MembershipFields, NaturalLinkFields, NullableLinkFields`; `82: MemberRelationSet`; `90: MemberRetrievalHooks`; `91: MemberRetrievalHooks`; `93: GroupRetrievalHooks`; `94: GroupRetrievalHooks` |
| `linkqueries/orm_test.go` | `36: MemberFields, QueryLinkMembers` |
| `nestedbindings/models.go` | `42: TeamRelationSet`; `43: ProjectFields, TeamFields, TeamRelationSet`; `45: ProjectRelationSet`; `46: ProjectFields, ProjectRelationSet, TaskFields, TeamFields`; `61: ProjectRetrievalHooks`; `62: ProjectRetrievalHooks` |
| `observerqueries/models.go` | `46: RecordChanges`; `91: RecordHooks`; `94: RecordHooks`; `97: RecordDraft`; `109: RecordChanges`; `115: RecordHooks`; `116: RecordDraft`; `123: RecordDraft`; `126: RecordDraft`; `133: RecordChanges`; `136: RecordChanges`; `139: RecordChanges`; `142: RecordChanges`; `148: RecordChanges` |
| `observerqueries/orm_test.go` | `35: NewRecordObserver, RecordHooks` |
| `pagequeries/legacy_clauses_postgres_test.go` | `15: QueryUsers`; `16: QueryOrders`; `17: OrderFieldsAt, UserFieldsAt`; `20: UserFieldsAt`; `21: OrderFieldsAt` |
| `recovering/hooks.go` | `66: MemberHooks`; `67: MemberHooks`; `68: MemberDraft`; `80: MemberChanges` |
| `retrievalqueries/hooks.go` | `29: MemberRetrievalHooks`; `30: MemberRetrievalHooks, QueryRetrievalMembers` |
| `retrievalqueries/models.go` | `53: MemberRelationSet`; `54: GroupFields, MemberFields, MembershipFields`; `55: MemberRelationSet` |
| `softqueries/hooks.go` | `17: MemberChanges`; `31: MemberHooks`; `34: MemberHooks`; `54: MemberChanges`; `55: MemberChanges`; `70: MemberHooks`; `71: MemberDraft`; `74: MemberDraft`; `75: MemberDraft`; `87: MemberChanges` |
| `softqueries/models.go` | `59: MemberRelationSet`; `60: GroupFields, MemberFields, MembershipFields`; `61: MemberRelationSet`; `68: MemberAggregateSet`; `69: MemberRelations`; `70: MemberAggregateSet` |
| `tenantqueries/models.go` | `56: DocumentFields`; `59: DocumentFields`; `66: DocumentRelationSet`; `67: DocumentFields, DocumentTagFields, TagFields`; `68: DocumentRelationSet`; `80: TagRelationSet`; `81: DocumentFields, DocumentTagFields, TagFields`; `82: TagRelationSet` |
| `testdata/compilefail/outbox_field_id/invalid.go` | `8: EventLinkDraft` |
| `timequeries/hooks.go` | `17: MemberDraft`; `21: MemberChanges`; `32: MemberHooks`; `34: MemberChanges`; `44: MemberHooks`; `45: MemberDraft`; `52: MemberDraft`; `61: MemberChanges` |

Direct generated factory callers (paths consumer-relative):

| Generated caller | Handwritten factory |
| --- | --- |
| `auditqueries/account_foundry.gen.go:1361` | `accountHooks` |
| `hookqueries/member_foundry.gen.go:1068` | `memberHooks` |
| `inputqueries/member_foundry.gen.go:958` | `memberHooks` |
| `linkqueries/archive_foundry.gen.go:1373` | `archiveHooks` |
| `linkqueries/archive_foundry.gen.go:1707` | `archiveReads` |
| `linkqueries/group_foundry.gen.go:1218` | `groupReads` |
| `linkqueries/member_foundry.gen.go:802` | `defineMemberRelations` |
| `linkqueries/member_foundry.gen.go:1324` | `memberReads` |
| `linkqueries/membership_foundry.gen.go:1189` | `membershipHooks` |
| `nestedbindings/project_foundry.gen.go:836` | `defineProjectRelations` |
| `nestedbindings/project_foundry.gen.go:1379` | `projectRetrieval` |
| `nestedbindings/team_foundry.gen.go:689` | `defineTeamRelations` |
| `observerqueries/record_foundry.gen.go:839` | `localHooks` |
| `recovering/member_foundry.gen.go:1100` | `memberHooks` |
| `retrievalqueries/member_foundry.gen.go:759` | `defineMemberRelations` |
| `retrievalqueries/member_foundry.gen.go:1282` | `memberRetrieval` |
| `softqueries/member_foundry.gen.go:853` | `defineMemberRelations` |
| `softqueries/member_foundry.gen.go:875` | `defineMemberAggregates` |
| `softqueries/member_foundry.gen.go:1164` | `memberHooks` |
| `tenantqueries/document_foundry.gen.go:930` | `defineDocumentRelations` |
| `tenantqueries/tag_foundry.gen.go:682` | `defineTagRelations` |
| `timequeries/member_foundry.gen.go:952` | `memberHooks` |
| `tenantqueries/document_foundry.gen.go:147` | `defineDocumentGlobalScopes` passed to WithGlobalScopeSource |

`auditqueries.Domain` and `EmailAfter` have no current handwritten caller. Old Trace/State members listed above are read/written only by their old factories/helpers; native tests use the ORM counterparts. Requested patch postimages add no new callers of those helpers.

## Saved-patch reconciliation

All filenames below are under `/private/tmp/claude-501/-Users-weiloon-Projects-rust-go-Foundry-Go/d36a1a88-907c-4231-9ebf-4636dd0c0c25/scratchpad/patches/`.

- `laneA-full2-on-fdaff938.patch`, postimage `httpmodels/bindings_postgres_test.go:142–181`: native Groups/Members/MemberLinks calls replace the worktree's inbound generated callers. Its historical reports still say nested negatives await D, but current `testdata/compilefail/nested_field_owner/invalid.go:8` and `nested_relation_owner/invalid.go:8` already use `TeamRecords.Name` and `TeamLinks.Projects`; those report strings are not callers.
- `laneD-full-on-79db1cf8.patch`: postimage `nestedbindings/models.go:43–47,62–63` retains old relation/retrieval wrappers; `nestedbindings/tables_test.go:14` adds a legacy QueryProjects/ProjectFields parity oracle. The latter is absent in the newer worktree, so this older patch must not overwrite current ownership blindly. No other reconstructed file introduces an owned generated-model call.
- `laneD-full-on-05b830c.patch` and the 19 `laneC-*` files: no additional handwritten call to an owned generated model/projection API found in reconstructed changed files. C's shared-model/SQL work is largely outside B ownership; current two DISTINCT ON tests still depend on those generated shared symbols. No requested C patch supplies the outstanding RecordRead adapter cutover.
- Neither D full patch changes `httppagination/pagination.go`; neither removes the EventLinkDraft negative. Patch report/catalogue text mentioning an old declaration is not proof that its source migrated.
- These are independently saved patches, many historical and overlapping, **not a synthesized branch**. Unchanged files retain their worktree findings. The lead must re-run the caller audit on the actual integrated tree at the barrier.

| Inspected patch | SHA-256 |
| --- | --- |
| `laneA-full2-on-fdaff938.patch` | `5c0970d0888c1ee819c767d6c90b3eabc5cb8483ae5c8e0d9aae095b775c671b` |
| `laneC-6a1-6a2.patch` | `b173fc63095f93578ba8677f8769a962be2230411d858ffbf18c543c6d0248e0` |
| `laneC-6a1.patch` | `3f9897945a7f48e7cfcedb4eb126d5a370647e3abe9768e1a5a9d6cb8d8ba580` |
| `laneC-6a3-only.patch` | `fa61a64f44ebb62bde9d686634b6ac55b71c3702103e51cce01063d387e57237` |
| `laneC-6a4-only.patch` | `b939078bf0c4bf8ddf29c7b06c760f90a8b0b6e2f11107066898b85ef794c711` |
| `laneC-6d.patch` | `b59c7f996e7da9a7670d3fbf364a67c5c926ae317998cfc2ead0f164f4a632d4` |
| `laneC-6d1.patch` | `dd6daa94a87f14ca5a759f29364974b26bef58f745c892e4d6967c847177f67f` |
| `laneC-6d3-only.patch` | `9d49e007987c1bee354eb00f5d88e5341fd62c7bd9857fd9c0c7e1619e017d0e` |
| `laneC-7caps-only.patch` | `8fa6ca531cd646a23bb71cb55cb06ca0c3dda59008ae3a660032dcc496559506` |
| `laneC-8a-only.patch` | `d5f78b7a84344cce0c3157aaaaa5f5bef6f5313d709043bedaea813f31f7958d` |
| `laneC-dt1-only.patch` | `de364e08c6988fe5f43f66c5348155c3e6d71e2455006b2103069aa7c62fa823` |
| `laneC-dt12-on-f3cfffd.patch` | `46d462b3f0c8cd0dd1432598c36496a12343998d76836bf00230292265ab6361` |
| `laneC-dt123-on-f3cfffd.patch` | `f83f874fb4e46a01f86c5306489b9276056acc28a7f82f539ea10d7259eaa6e7` |
| `laneC-full-backup.patch` | `fca6adfdcceb409addcc30df427037593f9d1df803637ad34f0f8e394b2c11b5` |
| `laneC-keyclauses-on-fdaff938.patch` | `c9282ecfde3916e0abb8cd140aa3147c699f73c95240fd42097c906cb71a5956` |
| `laneC-pg-each-fix.patch` | `77e8c1220d6a092cb4d8370ab25b59f6b59da002fd629be077dd44fdffdabb44` |
| `laneC-s9a1-on-6d80111.patch` | `57c8a7f263a78260342dcfec33ba5100e748121ed3bc0cc33ffd0a0d6f88ed14` |
| `laneC-s9clauses-on-6d80111.patch` | `ae8eb9810105484ee15869aed080060854a4d57d6a399b3c930fd9f3dd7e046c` |
| `laneC-s9resclauses-on-6d80111.patch` | `185a8b57807f62e818d0491740327051b2811e245aa9edba312349c25d94026d` |
| `laneC-s9w1-on-6d80111.patch` | `8af068b615dc13b9c110a58a47e19f782068d1ce52ee7d67e0f9f19b6a45b700` |
| `laneD-full-on-05b830c.patch` | `272a18114b98c78731d9dc5fb3b5b1ca9470b0aef4cc7de5cce26fe0f30b0afb` |
| `laneD-full-on-79db1cf8.patch` | `4b5bf510d72030fc0c5b77f918b321c118c16f1df22fd6e649f4662f28fdf858` |

## Barrier handoff

1. Integrate A's binding cutover and D's missing pagination artifact; C supplies RecordRead DISTINCT ON. B ports its two tests and outbox-ID negative; preserve native identity for recovery/Team and have C preserve User identity.
2. Reconcile the older D nestedbindings test with the newer B package. Remove dead coexistence factories/fields while keeping any old oracle support needed for the freeze.
3. Lead runs/fingerprints legacy oracles on the integrated tree, including the link creation-laziness test, link SQL parity and observer family rejection; preserve corresponding native assertions and frozen expectations. Freeze the wider goway/root footprint oracles under their owners too.
4. B removes its 38 model/projection directives and remaining legacy-only helpers. A's generator surgery supplies native persistence recognition; lead regenerates owned output/manifests. Retain DTO/enum/config outputs, handwritten domain types/mutators/accessors, and TypeScript/contracts byte-for-byte.
5. Lead verifies the integrated caller closure and intended compile-fail diagnostics. Pure `query` limits/type aliases (for example retrieval chunk limits) do not alone block 8c; leave runtime retirement to 8d.

No source cleanup, verification, PostgreSQL or Redis run was performed by this audit. The existing accepted read-completion delta is preserved.
