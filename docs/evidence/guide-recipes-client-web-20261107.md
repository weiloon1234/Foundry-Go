# Milestone 3 client-web recipe extraction

Implementation handoff, 2026-11-07, in `lane/m3-client-web`.
This record maps the four Starter skills, including their `this-app` regions,
to existing guide owners and consumer sources. It does not claim compiler,
runtime or milestone acceptance for this revision.

No tests, builds or verification commands ran. Changed Go sources were formatted
with `gofmt`. The renderer was attempted once for the six owned guides, but the
sandbox denied access to `/Users/weiloon/Library/Caches/go-build`. Per `COMMON.md`,
rendering is deferred to the lead. New source-bound fences are empty until that
run; existing pilot fences retain their previous rendering. No rendered source
was copied or edited by hand. No generated contracts or frozen baselines changed.

## Sources and ownership

Source line references below are to `SKILL.md` under
`/Users/weiloon/Projects/rust-go/Foundry-Go-Starter/.claude/skills/` as read on
2026-11-07. Fixture paths in the tables are relative to
`tests/fixtures/consumer/`. A `path#region` identifies the exact authored region.
Existing pilot ownership follows
the "Recipe ownership from the three Starter skills" section of
[the pilot evidence](guide-proof-pilot-20261107.md).

The matching ooo datatables skill retains the same typed filter and ticket
lifetime teaching. Boss Club's web-data skill confirms credential-publication
failure semantics, formatting/UI locale separation and unknown enum display.
These app copies supplied extraction inventory only; framework source and
consumer fixtures supplied API authority. No app repository changed.

### datatables

| Starter lines and recipe | Guide owner | Fixture region or retained owner |
| --- | --- | --- |
| 18–22: public DTO and handwritten SQL row destinations | [Declare a table](../guides/datatable.md#recipe-declare-table), [row schema owner](../guides/model-projections.md#recipe-declare-rows) | Existing `reporting/models.go#datatable`; pilot `reports/rows.go#user-summary` |
| 23–39: typed columns, codecs, sort/filter/search/export, enum and nullable cells | [Declare a table](../guides/datatable.md#recipe-declare-table) | `reporting/members.go#member-table` |
| 27–38: required/nullable text and ordered/equality filters | [Filter adapters](../guides/datatable.md#recipe-table-filter-adapters) | `reports/datatable_filters.go#nullable-ordered-filter`; member table owns the complete column example |
| 40–45: stable result key, explicit source mappings, visibility in Go | [Declare a table](../guides/datatable.md#recipe-declare-table) | `reporting/members.go#member-table` |
| 46–51, 117–119: typed joins and nullable outer-join text adapters | [Joined tables](../guides/datatable.md#recipe-join-group-tables), [outer-joined text](../guides/datatable.md#recipe-filter-outer-joined-text) | `reporting/orders.go#joined-table`; `reports/datatable_filters.go#outer-joined-text` |
| 35–37, 52–54, 117–119: grouped aggregates, HAVING, result-row stable key | [Grouped tables](../guides/datatable.md#recipe-join-group-tables) | `reporting/orders.go#grouped-table`; ordinary projection execution remains with [model projections](../guides/model-projections.md#execution-and-failures) |
| 55–58: catalog labels, enum choices and unknown received values | [Table metadata](../guides/client-contracts.md#recipe-table-bindings), [received values](../guides/client-contracts.md#recipe-received-values) | `clientcontracts/testdata/table-bindings/types.ts#table-enum-metadata`; `clientcontracts/testdata/guide-recipes.ts#received-values`; Go enum annotation owner remains [localization](../guides/localization.md#feature-labels) |
| 59–66, 109–116: explicit access declarations, no synthetic authorization discovery | [Server authority](../guides/datatable.md#recipe-authorize-table) | `reporting/authority.go#current-table-authority`; `reporting/http.go#register-tables`; route access is the existing `reporting/table_routes.go#datatable-7` example |
| 67–72, 120–123: shared route configuration, token tickets, concrete current authority | [Standard table routes](../guides/datatable.md#recipe-table-routes) | `reports/datatable_routes.go#table-routes-config`; existing `reporting/table_routes.go#datatable-7`; token adapter remains at its [token owner](../guides/tokens.md#websocket-handshake-tickets) |
| 73–74: table IDs and explicit operation associations | [Custom operation bindings](../guides/datatable.md#recipe-table-operation-bindings) | Existing `clientcontracts/table_bindings.go#datatable-3` and `#datatable-4` |
| 75–79: DTO generation versus SQL schema changes, complete contract export inputs | [Table declarations](../guides/datatable.md#recipe-declare-table), [export ownership](../guides/client-contracts.md#recipe-export-contracts) | `clientcontracts/contracts.go#manifest-sources`; `clientcontracts/guide_export.go#export-contracts` |
| 80–83, 124–125: typed query/export registries, optional export, declared/received row types | [Client table bindings](../guides/client-contracts.md#recipe-table-bindings) | Pilot `clientcontracts/testdata/table-bindings/types.ts#table-registries` and `#table-adapter` |
| 87–93: current guard through redemption, export authorization and bounded artifacts | [Standard routes](../guides/datatable.md#recipe-table-routes), [export artifacts](../guides/datatable.md#recipe-export-table) | `reports/datatable_routes.go#table-routes-config`; existing `reporting/reporting_postgres_test.go#datatable-2` |
| 94–98: direct ticket URL, credential separation and no parallel TS column schema | [Ticket downloads](../guides/http-downloads.md#recipe-ticket-download), [table bindings](../guides/client-contracts.md#recipe-table-bindings) | Existing ticket-authenticated route is included only by [authentication](../guides/authentication.md#ticket-authenticated-routes); table metadata region above supplies client consumption |

The typed app `Spec` wrapper, capability names, registration filenames, shared
React DataTable, presentation callbacks, row numbering, persistence keys and
portal layout remain app policy. The guide teaches native `Spec`, route options
and authority ownership; it does not introduce those application abstractions.

### client-contracts

| Starter lines and recipe | Guide owner | Fixture region or retained owner |
| --- | --- | --- |
| 8–9, 29–42: registered declarations own export; complete inputs, matching options and generated artifact ownership | [Export contracts](../guides/client-contracts.md#recipe-export-contracts) | `clientcontracts/contracts.go#manifest-sources`; `clientcontracts/guide_export.go#export-contracts`, `#export-command` |
| 47–49, 67–70: manifest provenance and compatibility in both directions | [Export contracts](../guides/client-contracts.md#recipe-export-contracts), [versioning](../guides/client-contracts.md#versioning-and-acceptance) | Same export sources; compatibility is prose, with existing consumer positive/negative type and version fixtures retained |
| 50–54, 98–102: portal surface selection, shared runtime and package entries | [Surface declarations](../guides/client-contracts.md#recipe-select-surfaces), [portal entries](../guides/client-contracts.md#recipe-portal-entries) | `clientcontracts/contracts_test.go#portal-surfaces`; pilot `clientcontracts/testdata/surfaces.ts#portal-entries` |
| 55–59: generated schema handles, entry-scoped metadata, framework-independent client | [Schema handles](../guides/client-contracts.md#recipe-schema-handles), [descriptors](../guides/client-descriptors.md#recipe-field-descriptors) | `clientcontracts/testdata/guide-recipes.ts#schema-handles`, `#field-descriptors` |
| 60–66: Fetch transport, credentials/cancellation, redirects and streamed request duplex | [Fetch transport](../guides/client-contracts.md#recipe-fetch-transport) and its following transport behavior section | Pilot `clientcontracts/testdata/fetch-transport.ts#fetch-client`; existing transport source/runtime cases retained |
| 71–74: LocaleMap, received string keys, `.at` and `.element` | [Wire values](../guides/client-contracts.md#recipe-wire-values), [received values](../guides/client-contracts.md#recipe-received-values) | Pilot `guide-recipes.ts#locale-fields`; new `guide-recipes.ts#received-values`, both in clientcontracts testdata |
| 73–74: Go presentation labels/hints/validation and generated enum labels | [Field presentation](../guides/client-descriptors.md#recipe-field-presentation), [enum labels](../guides/client-contracts.md#recipe-enum-labels) | `clientcontracts/dto.go#field-presentation`; `clientcontracts/presentation.go#parameter-presentation`; `clientcontracts/testdata/guide-recipes.ts#enum-labels` |
| 75–78: operation-specific error unions and code narrowing | [Client narrowing](../guides/http-application-errors.md#recipe-client-narrowing) | Pilot `clientcontracts/testdata/error-details.ts#http-application-errors-example-2` |
| 79–81: explicit table bindings, typed registries and guarded downloads | [Table bindings](../guides/client-contracts.md#recipe-table-bindings), [standard routes](../guides/datatable.md#recipe-table-routes) | Pilot table-binding regions and new route-configuration region listed above |
| 82–83: issue/outcome semantics | [Issue addresses](../guides/client-descriptors.md#recipe-issue-addresses), [form outcomes](../guides/client-forms.md#recipe-form-outcomes) | `clientcontracts/testdata/guide-recipes.ts#issue-addresses`, `#form-outcomes` |

The canonical export script/profile, export authorization, package name,
output directory, selected surfaces/locales and verification cadence remain in
the app. The full-profile warning is reconciled with framework manifest source
selection; no application profile or script was copied upstream.

### web-data

| Starter lines and recipe | Guide owner | Fixture region or retained owner |
| --- | --- | --- |
| 18–19, 28–32: memory-only tokens, cookie credential client, one replay in the same identity | [Portal session](../guides/client-contracts.md#recipe-portal-session), [session coordination](../guides/client-contracts.md#recipe-session-coordination) | Pilot `guide-recipes.ts#portal-lifecycle`; new `auth.ts#credential-lock` in clientcontracts testdata |
| 33–40, 45–46, 113–115: credential lock, publication failures, redacted coordination error | [Session coordination](../guides/client-contracts.md#recipe-session-coordination) | `clientcontracts/testdata/auth.ts#credential-lock`, `#coordination-failure` |
| 41–44: identity version before asynchronous auth effects | [Session coordination](../guides/client-contracts.md#recipe-session-coordination) | `clientcontracts/testdata/auth.ts#session-version` |
| 51–56: portal operations, shared helpers and strict schema-handle decoding | [Portal entries](../guides/client-contracts.md#recipe-portal-entries), [schema handles](../guides/client-contracts.md#recipe-schema-handles) | Pilot `surfaces.ts#portal-entries`; new `guide-recipes.ts#schema-handles` in clientcontracts testdata |
| 57–65: operation/status/code/envelope narrowing, no status-only business success | [Client narrowing](../guides/http-application-errors.md#recipe-client-narrowing) | Pilot `clientcontracts/testdata/error-details.ts#http-application-errors-example-2`; owner prose now links uncertain form outcomes |
| 66–67: release realtime handles | [Existing realtime lifecycle owner](../guides/client-contracts.md#realtime-lifecycle) | Existing `clientcontracts/testdata/runtime.mjs` and `surfaces.mjs`; this lane does not claim new realtime recipe extraction |
| 71–77: exact numeric values, local versus instant time, independent format locale and optional timezone | [Wire values](../guides/client-contracts.md#recipe-wire-values), [public environment](../guides/client-contracts.md#recipe-public-environment) | Pilot `guide-recipes.ts#wire-values`; new `guide-recipes.ts#public-environment` in clientcontracts testdata |
| 78–86: generated enum labels, numeric values, lossless unknown fallback | [Enum labels](../guides/client-contracts.md#recipe-enum-labels), [table enum metadata](../guides/client-contracts.md#recipe-table-bindings) | `clientcontracts/testdata/guide-recipes.ts#enum-labels`; `clientcontracts/testdata/table-bindings/types.ts#table-enum-metadata` |
| 88–93: configured catalog locale set, Accept-Language and translated client validation | [Localized client](../guides/client-contracts.md#recipe-localized-client) | `clientcontracts/testdata/guide-recipes.ts#localized-client`; translation loading remains with [browser catalogs](../guides/localization.md#browser-catalogs) |
| 94–97, 124: Go public environment fields, generated decoder and optional/fallback semantics | [Public environment](../guides/client-contracts.md#recipe-public-environment) | `clientcontracts/testdata/guide-recipes.ts#public-environment`; Go declaration remains solely at [generated configuration](../guides/generated-configuration.md#public-browser-configuration) |
| 98–104, 125–130: strict bootstrap DTO decoding using a portal schema handle | [Schema handles](../guides/client-contracts.md#recipe-schema-handles) | `clientcontracts/testdata/guide-recipes.ts#schema-handles`; runtime reader state, provider and cache policy remain app-owned |

TanStack/Zustand policy, query retries, editor freshness, feature API seams,
`connectSession`, `clientOptions(translations)`, translation adapters, login-as
policy and app environment variable names are not generated APIs. The guide now
explicitly identifies the options wrapper and auth package subpath as app choices.

### web-forms

| Starter lines and recipe | Guide owner | Fixture region or retained owner |
| --- | --- | --- |
| 18–21, 31–36: generated DTOs/validation, exact numeric and temporal wire values | [Wire values](../guides/client-contracts.md#recipe-wire-values), [form values](../guides/client-forms.md#recipe-form-values) | Pilot `clientcontracts/testdata/guide-recipes.ts#wire-values`, `#form-values` |
| 40–43: server phone normalization and validation with one extension policy | [Form presence and input semantics](../guides/client-forms.md#recipe-form-presence) routes to [phone](../guides/phone.md) | Existing phone guide owns `phone.Parse` and `validation/phone`; no duplicate client parser or phone declaration added |
| 44–45: PATCH changed fields and omitted/null/empty distinction | [Form presence](../guides/client-forms.md#recipe-form-presence) | `clientcontracts/testdata/guide-recipes.ts#form-presence`; prose makes changed-field construction the caller's responsibility |
| 46–54, 92–97: decoded issues, descriptor paths, nested/escaped pointers, visible summaries | [Issue addresses](../guides/client-descriptors.md#recipe-issue-addresses) | `clientcontracts/testdata/guide-recipes.ts#issue-addresses`; app feedback-map naming/deduplication remains local |
| 63–72: form-result status, no implicit retries, unknown/response-contract/cancellation outcomes and newer drafts | [Form outcomes](../guides/client-forms.md#recipe-form-outcomes) | `clientcontracts/testdata/guide-recipes.ts#form-outcomes`; existing `forms.mjs` retains runtime scenarios |

Shared controls, browser-native validation presentation, picker implementation,
React actions and pending `createForm` adoption remain app choices. The framework
recipe explains its controller without authorizing an application migration.

## Unresolved and deferred work

- No in-lane framework recipe has an unresolved owner. Compiler proof for the
  added sources and equality of the rendered views remain pending the lead.
- Rendering is deferred after the sandbox cache denial. Empty source-bound
  fences are intentional pending generated output, not completed rendered proof.
- Phone parsing, Go enum-label declarations, translation catalog loading and
  realtime cleanup remain at their existing guide owners. Their extraction and
  any new `recipe-*` anchors belong to the corresponding milestone lanes; this
  record does not claim new coverage for those sections.
- Existing pilot regions were reused by links or retained in place. They were
  not copied into another guide. No TypeScript input registration was needed:
  `guide-recipes.ts` and `auth.ts` already occur in `typescript-inputs.json`, and
  `table-bindings/types.ts` already occurs in that fixture's input list.

## Files changed

Owned guides:

- `docs/guides/datatable.md`
- `docs/guides/client-contracts.md`
- `docs/guides/client-descriptors.md`
- `docs/guides/client-forms.md`
- `docs/guides/http-application-errors.md` (client section only)
- `docs/guides/http-downloads.md`

Owned fixture sources:

- `tests/fixtures/consumer/reports/datatable_filters.go` (new)
- `tests/fixtures/consumer/reports/datatable_routes.go` (new)
- `tests/fixtures/consumer/clientcontracts/guide_export.go` (new)
- `tests/fixtures/consumer/clientcontracts/contracts.go`
- `tests/fixtures/consumer/clientcontracts/contracts_test.go`
- `tests/fixtures/consumer/clientcontracts/dto.go`
- `tests/fixtures/consumer/clientcontracts/presentation.go`
- `tests/fixtures/consumer/clientcontracts/testdata/guide-recipes.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/auth.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/table-bindings/types.ts`
- `tests/fixtures/consumer/httpdownloads/example.go`

Marker-only exceptions outside the owned fixture packages:

- `tests/fixtures/consumer/reporting/members.go`: existing concrete table declaration.
- `tests/fixtures/consumer/reporting/orders.go`: existing joined and grouped tables.
- `tests/fixtures/consumer/reporting/authority.go`: existing private guard-backed authority constructor.
- `tests/fixtures/consumer/reporting/http.go`: existing manager registration owner.

The owned `reports` package contains SQL row schemas, not these table declarations
or authority implementation. Marking their existing owners avoids duplicating
complete tables and policy code. The new focused filters and route assembly live
in the owned `reports` package. This evidence file is also new.

## Lead commands

Render the six changed guides first, then run the lead's selective checkpoint:

```sh
go run ./internal/cmd/renderdocs \
  --guide datatable.md --guide client-contracts.md \
  --guide client-descriptors.md --guide client-forms.md \
  --guide http-application-errors.md --guide http-downloads.md
make docs-check
(cd tests/fixtures/consumer && go test ./reports ./httpdownloads)
make typescript-check
```

Use the existing native toolchain and TypeScript environment. `make typescript-check` compiles the registered TS inputs against freshly exported
clients and runs the existing client/runtime/table scenarios. No new DTO or
transport declarations require contract regeneration before that command.
At the integrated milestone checkpoint, `make verify` covers fixture and client
proof; do not repeat the expensive client run merely to refresh its result.
