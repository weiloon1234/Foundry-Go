# Ticket-authenticated HTTP routes — B22

Implemented PLAN §2 on `lane/b22-tickets`, base `21f82017` (v0.3.3), for
v0.4.0. The requested targeted acceptance gate passed in retained segments.
No application, released cohort or frozen oracle changed. No commit, push,
merge, tag or external publication was performed. Only local `current-contracts`
were republished.

## Final public API: before → after

Package qualifiers below identify the owning package; `http` is Foundry HTTP.
All new declarations have doc comments. Datatable declarations retain compiled
examples and the required `Example:` lines.

| Before | After |
| --- | --- |
| `websocket.TicketRedeemer` | Moved to `auth.TicketRedeemer`, with the same methods: `TicketSource() auth.CredentialName` and `RedeemTicket(context.Context, secret.String) (value.Optional[auth.BoundCredential], error)`. No alias remains. |
| `websocket.WithTickets(redeemers ...websocket.TicketRedeemer) websocket.Option` | `websocket.WithTickets(redeemers ...auth.TicketRedeemer) websocket.Option` |
| `application.RealtimeDeclarations.Tickets []websocket.TicketRedeemer` | `application.RealtimeDeclarations.Tickets []auth.TicketRedeemer` |
| No ticket credential kind | `const http.TicketCredentialKind http.CredentialKind = "ticket"` |
| No typed ticket endpoint | `type http.TicketEndpoint[P, Q, B, M, R any] struct` with private fields |
| No ticket constructor | `func http.TicketAuthenticated[P, Q, B, M, R any](endpoint http.Endpoint[P, Q, B, R], binding http.GuardBinding[M], redeemer auth.TicketRedeemer, options ...http.GuardOption[M]) http.TicketEndpoint[P, Q, B, M, R]` |
| No ticket handler binding | `func (e http.TicketEndpoint[P, Q, B, M, R]) Handle(handler func(context.Context, M, http.Input[P, Q, B]) (R, error)) http.RouteRegistration` |
| No ticket URL boundary | `func (e http.TicketEndpoint[P, Q, B, M, R]) URL(ctx context.Context, path P, query Q, ticket secret.String) (string, error)` |
| No ticket declaration validator | `func http.ValidateTicketAuthenticated[P, Q, B, M, R any](e http.TicketEndpoint[P, Q, B, M, R]) error` |
| No ticket inspector | `func http.InspectTicketAuthenticated[P, Q, B, M, R any](e http.TicketEndpoint[P, Q, B, M, R]) (http.EndpointInfo, error)` |
| `type datatable.Tickets[M, A any] struct { Issue func(context.Context, M) (datatable.IssuedTicket, error); Redeem func(context.Context, secret.String) (A, error) }` | `type datatable.Tickets[M any] struct` with private issuance, redemption and validation fields; public `Issue` and `Redeem` fields removed |
| No token adapter | `func datatable.TokenTickets[M model.Identifiable, K any](tokens *token.Tokens[M, K]) datatable.Tickets[M]` |
| `datatable.RoutesConfig[M, A].Tickets datatable.Tickets[M, A]` | `datatable.RoutesConfig[M, A].Tickets datatable.Tickets[M]` |
| `type datatable.IssuedTicket struct { Secret secret.String; ExpiresAt temporal.DateTime }` | Removed, including both fields; token issuance owns the result |
| `datatable.DownloadQuery.Ticket string` | Removed; remaining fields are `Format datatable.ExportFormat`, `Request string`, `Locale value.Optional[i18n.LocaleID]` |
| No ticket capability validator | `func (t *token.Tokens[M, K]) ValidateTickets() error` (the receiver retains its existing `M model.Identifiable, K any` constraints) |
| `manifest.Version = 7` | `manifest.Version = 8` |

No compatibility aliases or deprecated leftovers were added. Existing
`datatable.Routes`, `ExportAccess`, `DownloadQueryDescriptor`, `CredentialInfo.Validate`
and `Tokens.IssueTicket` signatures are unchanged. Their behavior/documentation
now describes guarded ticket downloads, ticket credential metadata and the
explicit secret-to-URL boundary.

Compiled consumer helpers added (all previously absent):

- `reporting.NumericLevelTokenTickets(tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]]) datatable.Tickets[reporting.Operator]`
- `reporting.MemberTicketEndpoint(binding http.GuardBinding[reporting.Operator], tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]]) http.TicketEndpoint[http.NoPath, http.NoQuery, http.NoBody, reporting.Operator, http.Download]`
- `reporting.MemberTicketURL(ctx context.Context, binding http.GuardBinding[reporting.Operator], tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]], ticket secret.String) (string, error)`
- `reporting.MemberTicketRoute(manager *datatable.Manager, binding http.GuardBinding[reporting.Operator], tokens *token.Tokens[reporting.Operator, model.ID[reporting.Operator]]) http.RouteRegistration`

## PLAN §2.6 evidence

`TestPostgresTicketAuthenticatedRoutes` below lives in `auth/token/postgres` and
uses the existing real PostgreSQL token fixture, with required mode enabled.

| Required evidence | Test covering it |
| --- | --- |
| Success with actor and scopes | `TestPostgresTicketAuthenticatedRoutes` checks the concrete actor and `tokens.Current(ctx).Scopes()` in the successful handler |
| Missing, empty, repeated, oversized ticket | Same test, `malformed-does-not-consume`; includes an encoded repeated parameter name and verifies persisted ticket count is unchanged |
| Unknown, expired, replayed | Same test, `unknown-and-expired`, `head-and-replay` |
| Another guard's tokens | Same test, `wrong-guard`; verifies the rightful redeemer can still consume it afterward |
| Family revoked between issue and use | Same test, `revoked` |
| Disabled subject | Same test, `ineligible` |
| Missing export scope → 403 | Same test, `scope-ceiling` |
| Permission denial → 403 | Same test, `permission` |
| Authorization without a ticket → 401 | Same test, `malformed-does-not-consume`; every request includes a valid bearer header |
| HEAD → 405, unconsumed ticket | Same test, `head-and-replay`; checks `Allow: GET`, then successful GET, then replay denial |
| Redeemer error/panic → 503 | `http.TestTicketEndpointFailuresAndRecovery`; also Goexit, cyclic errors and panicking error methods, followed by a healthy request. `internal/authticket.TestRedemptionContainsErrorMethods` verifies bounded traversal and redaction |
| Query declares `ticket` → rejected | `http.TestTicketEndpointDeclarationAndMetadata` |
| Non-GET rejected | `http.TestTicketEndpointDeclarationAndMetadata` |
| Privacy headers on success/failure | Every PostgreSQL route response checks no-store/no-referrer/no challenge; `TestTicketPrivacyHeadersWinOverRouteMiddleware` and the real datatable download check override resistance |
| Typed query ignores consumed ticket | PostgreSQL test `url-and-typed-decoding` uses `EmptyQuery`; the datatable PostgreSQL test decodes its nonempty domain query |
| Manifest, OpenAPI, TypeScript | Consumer `TestTicketDownloadContracts`, strict reporting `types.ts`, reporting runtime branch of `TestTypeScriptClientAgainstRealHTTPAndWebSocket`, full `./typescript` |
| Deferred row loading resolves guard, locale propagated | `datatable.TestPostgresTokenRoutesKeepScopeThroughDeferredDownload`; actual token storage, current guard/scope during deferred source loading, locale and CSV headings/rows |
| WebSocket behavior unchanged | `websocket.TestTicketHandshakeAuthenticatesOnlyItsGuardAndRefreshRevokes`, `TestTicketsRequireHubAuthenticationAndUniqueSources`; `application.TestRealtimeTicketsAuthenticateBrowserSocketsUntilRevocation` with PostgreSQL |
| Wrong model's TokenTickets rejected | Consumer and Go-way `TestInvalidPublicContractsFailCompilation/datatable_routes_ticket_actor` |

Additional tests cover sticky validation, zero/nil bindings and redeemers, source
mismatch, invalid options, ticket metadata validation, actor middleware and
attribution, cookie/CSRF bypass, disabled ticket configuration and nil token
bindings. Existing datatable lifecycle, authorization-before-issuance, URL bounds,
quota, cancellation, callback ownership and timeout tests passed.

Real gopls completion, hover and definition probes cover `ticket-http-endpoint`,
`ticket-http-url` and `ticket-datatable-model` in the compiled consumer.

## Acceptance commands and results

Commands ran natively with `GOFLAGS=-trimpath`, warm caches and approximately
141 GiB disk headroom before the gate. Approved sandbox escalation allowed native
cache, local listener and existing database access. No dependency was installed.
Raw logs are retained locally in `/tmp/b22-acceptance/`.

PostgreSQL commands sourced the user-specified private file without displaying
credentials, then explicitly exported `FOUNDRY_TEST_POSTGRES_URL` and
`FOUNDRY_TEST_POSTGRES_REQUIRED=1`. Sourcing alone did not export the URL; the
first required-backend run failed rather than silently skipping. Only affected
packages were resumed. Existing services and isolated fixture data were used;
no reset, drop, truncate or duplicate server was run.

TypeScript commands used `FOUNDRY_TEST_NODE=$(command -v node)` and
`FOUNDRY_TEST_TYPESCRIPT=/Users/weiloon/Projects/rust-go/Foundry-Go/tools/typescript/node_modules/typescript/lib/tsc.js`.
The client gate enables `FOUNDRY_TEST_TYPESCRIPT_REQUIRED=1`.

| Command (root unless noted) | Result |
| --- | --- |
| `go build ./...` | PASS on final source |
| `go vet ./http/... ./auth/... ./websocket/... ./datatable/... ./openapi/... ./contract/... ./application/... ./internal/authticket` | PASS on final source |
| `go vet ./internal/agent` | PASS |
| Consumer: `go vet ./reporting ./clientcontracts` | PASS |
| `go test -timeout=20m -count=1 ./http/... ./auth/... ./websocket/... ./datatable/... ./openapi/... ./contract/... ./application/... ./internal/authticket` | Segmented PASS; required PostgreSQL failures resumed in the next row |
| `go test -timeout=20m -count=1 ./http ./auth/challenge/postgres ./auth/mfa/postgres ./auth/session/postgres ./auth/token/postgres ./datatable ./application` | PASS with exported PostgreSQL URL and required mode |
| `go test -timeout=20m -count=1 ./http ./websocket ./datatable ./application` | PASS after final response-writer refinement, PostgreSQL required |
| `go test -timeout=20m -count=1 ./auth/token/postgres -run '^TestPostgresTicketAuthenticatedRoutes$'` | PASS after final response-writer refinement, PostgreSQL required |
| `go test -timeout=20m -count=1 ./typescript` | Full package PASS after current-contract publication, with native Node/TypeScript configured |
| `make generate-check` | PASS, all 15 configured roots |
| `make publish-client-contracts` | PASS, only current cohort |
| Consumer: `go test -timeout=20m -count=1 ./clientcontracts ./reporting` | Segmented PASS; reporting passed, the single new metadata assertion was corrected and resumed below |
| Consumer: `go test -timeout=20m -count=1 ./clientcontracts -run '^TestTicketDownloadContracts$'` | PASS |
| `make typescript-check` with the specified tools | Segmented PASS; strict clients, HTTP/WebSocket and React/Vue adapters passed; reporting runtime assertion resumed below |
| Consumer: `go test -timeout=20m -count=1 -v ./clientcontracts -run '^TestTypeScriptClientAgainstRealHTTPAndWebSocket$/^reporting$'` | PASS with required native TypeScript tools |
| Go-way: `go test -timeout=20m -count=1 ./guideadapters ./guideexamples` | PASS |
| `go test -timeout=20m -count=1 . -run '^TestConsumerAPI(Documentation\|MethodVisibility\|DSLGoDocSurface)$'` | PASS |
| Consumer and Go-way, separately: `go test -timeout=20m -count=1 . -run '^TestInvalidPublicContractsFailCompilation$/^datatable_routes_ticket_actor$'` | PASS in both modules |
| `go test -timeout=20m -count=1 ./internal/agent -run '^(TestConsumerEditorProbeAnchors\|TestRealGoplsConsumer)$/(ticket-http-endpoint\|ticket-http-url\|ticket-datatable-model)$'` | PASS with `FOUNDRY_TEST_GOPLS=/Users/weiloon/Projects/rust-go/Foundry-Go/bin/gopls` |
| `make release-tools-check` | Segmented PASS; release Go vet/tests and other Python suites passed; catalogue correction resumed below |
| `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'goway_catalogue_test.py'` | PASS, 12 tests; together with the original 26 passing Python tests, all 38 passed |
| `make docs-check` | PASS |
| `git diff --check` | PASS |

Initial compile corrections were a missing `auth` import in application, an
incorrect package qualifier in a same-package manifest test, and a missing return
after a test callback's `runtime.Goexit`. Assertions were corrected for the
successful-handler count, escaped embedded TypeScript metadata and the existing
lossless runtime `JSONNumber.text` representation of manifest version 8.
These did not change the required runtime guarantees. The initial full TypeScript
package run correctly rejected stale manifest-7 current contracts before publication.

The compile-negative case initially declared its own model identity method; the
catalogue scanner treats that name as a retired generated symbol. Both mirrors
now use existing concrete fixture models and still reject the intended actor
type mismatch. The existing `datatable_routes_ticket_actor` catalogue entry was
repurposed for the replacement API: both `compiler_cases_test.go` entries already
cover it, the sorted mapping and README were updated, and the existing 1,058 count
in `tools/goway_catalogue_test.py` remains correct. No new non-Go testdata path
required a release-source allowlist change.

Generation initially could not compile against the stale generated
`DownloadQuery.Ticket` field. The unchanged framework generator was built from a
read-only archive of base `21f82017` into `/tmp/b22-framework-toolchain` using
`go build -o /tmp/b22-acceptance/foundry ./cmd/foundry`, then run in this worktree
as `/tmp/b22-acceptance/foundry generate --recursive --field-docs --dir datatable`.
The current framework's `make generate-check` subsequently verified identical
output. No generated artifact was hand-edited.

## Review and deviations

The initial review incorrectly claimed no unresolved deviation from PLAN §2.
Independent review of `61cd8bb5` found B22-1: native routing responses bypassed
ticket privacy, and canonical redirects echoed an unconsumed ticket. The
follow-up below records the correction and the strengthened evidence. The user-approved
`TokenTickets[M model.Identifiable, K any]` constraint is now in §2.1; `Tickets[M any]`
remains unchanged. The only extra public capability check is
`Tokens.ValidateTickets`, expressly allowed by §2.5 to reject disabled tickets
before registration.

Source review traced declaration validation, isolated shared redemption, guard
resolution, request-scope closure after response writing, consumed-query decoding,
HEAD interception and privacy header enforcement. HTTP uses the same guard
requirements, actor middleware, attribution and failure writer as existing
authenticated endpoints. The existing response capability wrapper preserves
streaming/native controls while fixing headers before commitment. Redeemer
source lookup is inside the shared isolated callback. No separate token or
authorization implementation was introduced.

The authentication guide documents HTTPS, upstream query-log redaction, browser
history exposure, no access/refresh tokens in URLs, lack of resource/filter/route
binding, current resource authorization, rate limits, privacy headers and fresh
tickets for retry/Range/If-Range after consumption.

Full `make verify`, race suites, the fresh independent read-only pre-merge review,
application migrations and release tagging are outside this implementation gate
and are not claimed. No performance claim is made.

## Changed files, grouped

The inventory and source fingerprint below are recorded after final source review.

### Guides, compatibility, catalogue and evidence

- `CHANGELOG.md`
- `blueprint/00-master-architecture-and-parity.md`
- `blueprint/goway-data-layer/README.md`
- `blueprint/goway-data-layer/compilefail-mapping.tsv`
- `docs/compatibility.md`
- `docs/evidence/ticket-routes-20261101.md`
- `docs/guides/authentication.md`
- `docs/guides/datatable.md`
- `docs/guides/http-downloads.md`
- `docs/guides/tokens.md`
- `docs/guides/websocket.md`

### Framework implementation

- `application/declarations.go`
- `auth/ticket.go`
- `auth/token/ticket.go`
- `contract/manifest/document.go`
- `datatable/routes.go`
- `datatable/routes_transport.go`
- `http/authentication.go`
- `http/authentication_binding.go`
- `http/credential_metadata.go`
- `http/observed_response.go`
- `http/router.go`
- `http/ticket_endpoint.go`
- `internal/authticket/redeem.go`
- `openapi/openapi.go`
- `websocket/hub.go`
- `websocket/ticket.go`

### Framework regression tests

- `application/realtime_ticket_test.go`
- `auth/token/postgres/ticket_http_test.go`
- `contract/manifest/table_operations_test.go`
- `datatable/routes_test.go`
- `datatable/routes_ticket_postgres_test.go`
- `http/ticket_endpoint_test.go`
- `internal/authticket/redeem_test.go`
- `websocket/public_error_graph_test.go`

### Generated datatable DTO

- `datatable/.foundry-gen.json`
- `datatable/download_query_foundry.gen.go`

### Editor acceptance

- `internal/agent/gopls_acceptance_test.go`

### Independent consumers and compiler mirrors

- `tests/fixtures/consumer/README.md`
- `tests/fixtures/consumer/clientcontracts/reporting_contracts_test.go`
- `tests/fixtures/consumer/clientcontracts/testdata/reporting/runtime.mjs`
- `tests/fixtures/consumer/clientcontracts/testdata/reporting/types.ts`
- `tests/fixtures/consumer/reporting/table_routes.go`
- `tests/fixtures/consumer/reporting/ticket_route.go`
- `tests/fixtures/consumer/testdata/compilefail/datatable_routes_ticket_actor/invalid.go`
- `tests/fixtures/goway/testdata/compilefail/datatable_routes_ticket_actor/invalid.go`

### Published current client contracts

- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/.foundry-gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_browser_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_live_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_manifest_foundry.gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_members_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_openapi_foundry.gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/client/contracts_runtime_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/provenance.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/.foundry-gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_exports_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_manifest_foundry.gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_openapi_foundry.gen.json`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_reports_foundry.gen.ts`
- `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/reporting/contracts_runtime_foundry.gen.ts`

Changed source/contract fingerprint (SHA-256 of sorted relative path, NUL, bytes,
NUL; `.go`, `.ts`, `.mjs`, `.py`, `.tsv`, `.json`; excludes documentation):
`b00f5aa5d282e152267610ad6c219339d68ad04e94fadc6401aa77e0b6071dca`.


## Independent review follow-up — B22-1

The read-only review of `61cd8bb5` found one Medium defect and seven test blind
spots. Its source inspection correctly identified that native path redirects and
method failures bypassed the matched-route wrapper. Earlier PASS results remain
historical test results; they did not establish privacy on those routing paths.

B22-1 is corrected at the router boundary. Authentication and routing share one
bounded decoded-name iterator and the existing unexported `ticketParameter`
constant. At `03d622b1`, router errors for any ticket-bearing request enforced
no-store/no-referrer, and native redirects became ordinary 404 responses without
copying staged headers or bodies. Re-review found this rule was too broad for
domain parameters named `ticket`; B22-2/B22-3 below supersede that policy. Matched
ticket routes retain their existing privacy and authentication policy.

| Finding or blind spot | Strengthened evidence |
| --- | --- |
| B22-1; canonical-only privacy coverage | `TestPostgresTicketAuthenticatedRoutes/routing-responses-preserve-ticket`: repeated-slash and trailing-slash redirects, POST/OPTIONS 405, unknown-path 404 and ambiguous-path 400; ordinary and encoded parameter names; each with/without router-wide public CacheControl; committed headers, body non-disclosure, no Location and subsequent real ticket redemption |
| Mutable header assertions | `TestTicketPrivacyHeadersWinOverRouteMiddleware`, `TestTicketEndpointFailuresAndRecovery` and `TestPostgresTicketAuthenticatedRoutes` inspect `Result().Header` |
| Missing isolated download scope evidence | `TestPostgresTokenRoutesKeepScopeThroughDeferredDownload/download-export-access-independent-of-table-authorization/scope`: missing scope denies before table authorization, while a control route without ExportAccess permits the same actor/ceiling |
| Missing isolated download permission evidence | The same subtest's `permission` case: denied actor permission, independently permissive table authorization and successful control download |
| Streaming scope lifetime and closure | `TestTicketScopeSurvivesStreamingAndCloses`: guard lookup on each bounded CSV row read, a later chunk after bytes are written, lookup during body closure, then failed lookup on a captured context after success/error/panic; `context.WithoutCancel` prevents request cancellation from masking a leaked scope |
| Scheme exists but operation could be public | Consumer `TestTicketDownloadContracts` requires exactly one security alternative referencing the query ticket apiKey scheme, with no anonymous alternative |
| Cookie-only browser bypass evidence | `TestTicketEndpointBypassesBrowserSessionAdapter` uses `NewBrowserSessions` with the existing PostgreSQL session fixture; the ordinary authenticated control rejects insecure repeated malformed cookies, while the ticket route resolves its actor without session loading or Set-Cookie |

`TestRouterTicketPrivacyLeavesMatchedPublicRoutesUnchanged` additionally covers
empty ticket parameters, decoded aliases, ordinary non-ticket redirects and a
matched public route carrying an unrelated ticket parameter.
`TestRouterTicketPrivacyOnNativeError` covers the mux's native 400 passthrough
branch. The authentication guide now explains routing errors and redirect
suppression.

No public API, generated schema or contract artifact changed in this follow-up.
The fresh read-only review has occurred; this follow-up does not claim a separate
review of its corrections. No commit was made.

### Follow-up acceptance

Native commands used `GOFLAGS=-trimpath` and the existing warm cache (137 GiB
free at entry). PostgreSQL tests sourced
`/Users/weiloon/Projects/rust-go/Foundry-Go/.env.test`, then exported
`FOUNDRY_TEST_POSTGRES_URL` and `FOUNDRY_TEST_POSTGRES_REQUIRED=1`. No database
reset, destructive cleanup or duplicate service was used. Logs are retained at
`/tmp/b22-review-fixes/`.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS, including final source |
| `go vet ./http ./auth/token/postgres ./datatable` | PASS, including final source |
| Consumer: `go vet ./clientcontracts` | PASS |
| `go test -timeout=20m -count=1 ./http ./auth/token/postgres ./datatable` | PASS with PostgreSQL required; rerun after native-error passthrough coverage |
| `go test -timeout=20m -count=1 ./auth/token/postgres -run '^TestPostgresTicketAuthenticatedRoutes$'` | PASS after making handler-count assertions independent of subtest selection |
| Consumer: `go test -timeout=20m -count=1 ./clientcontracts -run TestTicketDownloadContracts` | PASS |
| `go test -timeout=20m -count=1 . -run '^TestConsumerAPI(Documentation\|MethodVisibility\|DSLGoDocSurface)$'` | PASS |
| `make docs-check` | PASS |
| `git diff --check` | PASS |

The package tests passed on the first run. Final source review added protection
for native mux errors other than staged 404/405 responses; the affected package
batch passed again. The PostgreSQL test now checks handler invocation counts per
request, allowing focused subtests to run independently.

### Mutation checks

Six temporary Go overlays deliberately restored broken behavior without editing
working source. Every selected regression test failed for the intended reason;
none failed compilation. These expected failures are evidence that the assertions
distinguish the guarantee, not acceptance failures.

Each command used `go test -timeout=20m -count=1
-overlay=/tmp/b22-review-fixes/mutations/<name>.json <package> -run '<pattern>'`.
PostgreSQL required mode and the native cache settings above remained enabled.

| Overlay name | Package / test pattern | Observed failure |
| --- | --- | --- |
| `router-before-fix` (router from `61cd8bb5`) | `./auth/token/postgres`, `^TestPostgresTicketAuthenticatedRoutes$/^routing-responses-preserve-ticket$` | Repeated-slash URL returned 307 instead of 404 |
| `download-without-export-access` | `./datatable`, `^TestPostgresTokenRoutesKeepScopeThroughDeferredDownload$/^download-export-access-independent-of-table-authorization$` | Both scope and permission cases returned 200 instead of 403 |
| `scope-not-closed` | `./http`, `^TestTicketScopeSurvivesStreamingAndCloses$` | Captured scope remained usable after success, handler error and handler panic |
| `browser-wrapper-runs` | `./http`, `^TestTicketEndpointBypassesBrowserSessionAdapter$` | Ticket route failed to bypass the actual browser adapter |
| `headers-not-pinned-at-commit` | `./http`, `^TestTicketPrivacyHeadersWinOverRouteMiddleware$` | Committed privacy headers were overwritten |
| `operation-without-security` | Consumer `./clientcontracts`, `^TestTicketDownloadContracts$` | Download operation had no required ticket scheme |

The corrections change only `http/router.go`, `http/ticket_endpoint.go`, their
regression tests, `auth/token/postgres/ticket_http_test.go`,
`datatable/routes_ticket_postgres_test.go`, the consumer contract test, the
authentication guide, this evidence and master status. Released cohorts, frozen
oracles and generated files are untouched.


Follow-up changed-source SHA-256 (sorted relative path, NUL, bytes, NUL;
source/contract extensions from the original fingerprint, excluding docs):
`6cdd8eae80b95ed9597167d5291240460a79b30d9f9c93c5e95d4db510a5b087`.


## Re-review follow-up — B22-2 and B22-3

The read-only re-review of `03d622b1` confirmed B22-1 and all seven original test
blind spots were closed. It identified two regressions caused by the preceding
lead decision's broad parameter-presence rule. The replacement decision scopes
routing protection to ticket-route ownership.

The router records ticket-route patterns from its registrations. For a request
with a decoded `ticket` query parameter, it asks `ServeMux.Handler` for the pattern
on a GET clone. The mux owns canonicalization and redirect-destination matching;
there is no second matcher or parsing of pattern strings. Only a pattern in the
registered ticket set activates routing-response privacy and redirect suppression.

| Fix | Evidence |
| --- | --- |
| B22-2: restore non-ticket redirects | `TestRouterTicketPrivacyLeavesMatchedPublicRoutesUnchanged` now requires the original trailing-slash 307 and exact Location for domain ticket parameters, empty values and encoded names. `TestTicketRoutingPreservesDomainTicketParameters` repeats the redirect control in a router that also contains a ticket route, with public CacheControl and outer headers |
| B22-3: preserve non-ticket routing headers | `TestTicketRoutingPreservesDomainTicketParameters` compares committed headers and body against the no-query baseline for POST/OPTIONS 405 and unknown-path 404; outer Referrer-Policy and WWW-Authenticate must survive |
| Retain ticket-route privacy and non-consumption | `TestPostgresTicketAuthenticatedRoutes/routing-responses-preserve-ticket` retains repeated-slash, dot-segment, trailing-slash, POST/OPTIONS and ambiguous-path checks, committed privacy headers, no Location or secret body, public-cache variants, decoded aliases and subsequent real redemption |
| Native routing errors remain protected for ticket destinations | `TestRouterTicketPrivacyOnNativeError` now registers an actual ticket route and exercises the native mux error for that destination |
| Unknown paths remain ordinary routes | The mixed-router control compares the complete committed response to its no-query baseline. The PostgreSQL unknown-path case retains the non-consumption assertion but expects the ordinary Referrer-Policy behavior |

The authentication guide now states this scoped rule. Public API signatures,
generated output, released cohorts and frozen oracles are unchanged. No commit
was made. Earlier acceptance and mutation results describe their recorded source;
the preceding broad-policy claims are historical, not the current contract.

### Re-review acceptance

Commands use native tools and `GOFLAGS=-trimpath`. PostgreSQL tests source
`/Users/weiloon/Projects/rust-go/Foundry-Go/.env.test`, explicitly export
`FOUNDRY_TEST_POSTGRES_URL`, and set `FOUNDRY_TEST_POSTGRES_REQUIRED=1`.
Logs are retained in `/tmp/b22-route-scope/`. No database reset or destructive
cleanup was performed.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./http` | PASS |
| `go test -timeout=20m -count=1 ./http ./auth/token/postgres ./datatable` | Segmented PASS: PostgreSQL token and datatable packages passed; HTTP resumed below after a test compile correction |
| `go test -timeout=20m -count=1 ./http` | PASS with PostgreSQL required |
| `go test -timeout=20m -count=1 . -run '^TestConsumerAPI(Documentation\|MethodVisibility\|DSLGoDocSurface)$'` | PASS |
| `make docs-check` | PASS |
| `git diff --check` | PASS |

The first HTTP test compilation rejected a variadic call to the existing
single-route helper. The new mixed-route fixture now calls `http.NewRouter`
directly; the full HTTP package passed on rerun. No runtime fix was needed
after the initial verification.

Re-review changed-source SHA-256 (sorted relative path, NUL, bytes, NUL;
source/contract extensions from the original fingerprint, excluding docs):
`364b4700358d73a44eafc0e2cdd6b90cdf3a3bb8ef7c430bc42ba62066af56b5`.
