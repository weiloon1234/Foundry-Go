# Stage 9 lane A: A3 and the HTTP cluster

Base `6d80111`, laneA9 only. The accepted Stage 7 remainder was reapplied and its
two registry conflicts resolved by union before Stage 9 work. Remainder acceptance
passed: 792 compiler cases, 130 probes, unchanged ceilings and identical exported
contracts. The old laneA worktree remains untouched for H1.

## Package inventory and disposition

No unblocked package needs a generated model getter/query rewrite. Retain their
ordinary Go models/IDs, enum/DTO/config/query/path generation and service contracts;
adding ORM declarations to their public signatures would create an unnecessary
model-declaration dependency. No positive source or generated output was changed.

| Packages | State and retained contract |
| --- | --- |
| `plugin_base` → `plugin_dep` → consumer `pluginusage` | Publish-ready retained: independent modules, dependency/lifecycle order, routes, jobs, events, policies, assets, migration history. The report scaffold stays an ordinary struct. |
| `background`, `scheduling` | Publish-ready retained: concrete model IDs and job payloads, scheduling and worker ownership. |
| `configured`, `developeroperations`, `httpassets`, `spaportals`, `startupconfig` | Publish-ready retained: application/configuration, diagnostics and transport services; retained config generation. |
| `httpcompression`, `httpcookies`, `httpcors`, `httpdownloads`, `httpdto`, `httpendpoints`, `httpetags`, `httpkernel`, `httpmiddleware`, `httpproxy`, `httpquery`, `httpsecurity`, `httpsigned`, `httpstreams`, `httpvalidation` | Publish-ready retained: model IDs, enums, DTOs, request/response and transport behavior. `httpetags` continues to consume lane D's `httppagination.PresentMember`; its presenter migration remains D-owned. |
| `tooling` | Blocked: A1 must publish Users/WriteRecords ORM tables and insert inputs. Replace `Records`' generated WriteRecordDraft factory and `UserPlan`, `LockedUserPlan`, `ReadUsers`, `ReadPrimaryUsers` generated queries. Preserve factory recipes, explain/transaction restrictions and routed-read assertions. Other tooling capabilities are retained. |
| `httpmodels` | Blocked: A1 Users/Orders/Countries/WriteRecords plus lane B retrieval/soft-delete declarations. Port `UserByID`, `CountryByCode`, `OrderWithinUser` and persisted binding tests; preserve no-SQL rejection, retrieved hooks, stored fields and soft-delete visibility. |

A2 remains deferred until the lead confirms A1 integration: officequeries,
pivothooks, relationshipfilters, productionprofile and its commands,
configuredprofile and its command, bootstrap and its serve command. A1 belongs to C.
No framework capability gap was found; the blockers are unpublished declarations.

## Negative cases and catalogue

Decision 4 transfers the 112 cases in the exact `HTTP, DTOs and transport contracts`
area. Together with the other A-owned areas there are 210 cases: 184 already use
retained public contracts; 26 still need supplier declarations. All 112 transferred
HTTP/DTO/transport cases are in the retained group. A Go-way counterpart does not
count as porting an unchanged legacy consumer negative.

| Supplier | Consumer negative ports awaiting its declarations |
| --- | --- |
| C: models | aggregate_input, aggregate_nonnullable_result, aggregate_owner, aggregate_pivot_input, aggregate_result, pivot_key, pivot_model, pivot_order, pivot_scope, relation_exists_owner, relation_key, relation_owner, relation_scope, relation_target, where_doesnt_have_owner, where_has_aggregate, where_has_find_owner, where_has_owner |
| B: linkqueries | relation_attach_draft, relation_attach_source, relation_attach_target, relation_create_defaults_owner, relation_detach_result |
| B: softqueries | soft_pivot_scope |
| D: nestedbindings | nested_field_owner, nested_relation_owner |

The catalogue now has 548 ported / 444 unaffected / 34 pending rows. None of the
pending Stage 7 rows belongs to A under the revised rules. New Stage 9 ports: **0**;
retained cases keep their existing dispositions and intended diagnostics. The
pagination/model-binding negatives classified as `Framework integrations` remain
D-owned, consistent with the area-based rule; this turn does not edit them.
The evidence JSON lists every assigned case, diagnostic and dependency.

## Acceptance and handoff

All 21 targeted acceptance commands passed on r6, GOMAXPROCS=4, GOFLAGS=-p=4, GOWORK=off.
Plugin modules passed build/vet/tests/races base then dependent, then consumer
pluginusage. The remaining A3/HTTP packages passed build/vet/tests/races; all 210
owned compiler cases passed with their intended diagnostics. Catalogue, docs-check,
repository gofmt and diff checks passed. The preceding remainder exports remain
valid because Stage 9 changed no runtime/declaration/generated source. All 324
recorded package/negative source fingerprints match. Blocked packages/cases
are tested as retained legacy baselines, not reported as migrated ORM consumers.
PostgreSQL remains compile/vet only; the lead owns live integration gates.

No Stage 9 source rewrite was necessary in the 25 retained-ready packages. No
Stage 9 API, schema, generated output, pin, catalogue or footprint changes.
Existing ceilings and historical measurements remain unchanged. No commit.

## Requests and Docs notes

Publish A1 table/input/relation/aggregate declarations from C, link/soft/retrieval
surfaces from B, and nestedbinding fields/relations from D before the blocked
consumer cutovers. Signal A1 integration before releasing A2. The retained
package/negative roster can be marked ready after the lead accepts these checks;
no generator retirement or cleanup is implied.
