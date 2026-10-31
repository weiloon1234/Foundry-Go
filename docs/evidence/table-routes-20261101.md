# Framework-owned table routes — v0.3.2

Implemented Part B of the approved table-bindings design on `lane/table-routes`,
base `425137ef` (v0.3.1). No application repository was changed. No commit, push,
merge, tag or external publication was performed. Only the local
`current-contracts` cohort was republished.

## Public API

The [implementation](../../datatable/routes.go) exposes package functions and
configuration types; no methods were added to generic types. `http` below is the
framework HTTP package.

```go
func Routes[S, R, M, A any](
    group http.Scope,
    table Table[S, R, A],
    config RoutesConfig[M, A],
    options ...RouteOption[M],
) ([]http.RouteRegistration, error)

func ReadAccess[M any](options ...http.GuardOption[M]) RouteOption[M]
func ExportAccess[M any](options ...http.GuardOption[M]) RouteOption[M]

type RoutesConfig[M, A any] struct {
    Manager    *Manager
    Guard      http.GuardBinding[M]
    Authority  func(context.Context, M) (A, error)
    Tickets    Tickets[M, A]
    Clock      clock.Clock
    Quota      http.Middleware
    WithLocale func(A, i18n.LocaleID) A
}

type Tickets[M, A any] struct {
    Issue  func(context.Context, M) (IssuedTicket, error)
    Redeem func(context.Context, secret.String) (A, error)
}

type IssuedTicket struct {
    Secret    secret.String
    ExpiresAt temporal.DateTime
}
```

`RouteOption[M]` retains private immutable option data. The framework owns
[ExportRequest, DownloadQuery and ExportTicket](../../datatable/routes_transport.go).
The existing generator produces these public descriptors and field sets:

```go
func ExportRequestJSON() contract.JSON[ExportRequest]
func ExportRequestValidationFields() ExportRequestValidationFieldSet
func DownloadQueryDescriptor() http.Query[DownloadQuery]
func ExportTicketJSON() contract.JSON[ExportTicket]
func ExportTicketValidationFields() ExportTicketValidationFieldSet
```

`ExportRequestValidationFieldSet` contains concrete `Format` and `Request` fields;
`ExportTicketValidationFieldSet` contains concrete `URL` and `ExpiresAt` fields.
Every new exported declaration is documented. `go doc` confirmed the route,
configuration and callback signatures.

## Changed files by responsibility

- Framework: `datatable/routes.go`, `routes_transport.go`, the three generated
  descriptor files and `datatable/.foundry-gen.json`.
- Runtime regressions: `datatable/routes_test.go`.
- Consumer/client evidence: `tests/fixtures/consumer/reporting/table_routes.go`,
  `clientcontracts/reporting_contracts_test.go`, existing reporting TypeScript and
  runtime fixtures, and generator-published current reporting contracts/provenance.
- Compiler contracts: three `datatable_routes_*` cases in both consumer and
  Go-way fixtures, their catalogues, the sorted mapping, README summary/count and
  `tools/goway_catalogue_test.py`.
- Documentation: datatable guide, compatibility notes, consumer README, changelog,
  master status and this evidence record.

No new non-Go testdata directory or source allowlist change was needed. Released
`contracts-v0.3.0`, `contracts-v0.3.1`, `datatable-baseline`,
`native-cursor-contracts`, frozen oracles and the undocumented API baseline are
unchanged.

## Tests added

`TestRoutesValidateDeclarationsAndSnapshotAccess` covers missing/invalid manager,
guard, authority, clock, issuer, redeemer, quota, scope, exact table identity,
invalid/duplicate options, option slice snapshots, 403 before malformed body
decoding for denied scopes and permissions, and route/middleware metadata.

The four PostgreSQL tests exercise actual framework HTTP routing and existing
isolated report schemas:

- `TestPostgresRoutesTicketLifecycle`: invalid input before issuance, table
  authorization before bounded validation, pre- and post-issuance URL limits,
  ticket URL/expiry, negotiated locale in the URL and deferred source/CSV labels,
  disabled/revoked/expired/replayed credential errors, download authorization,
  credential headers and secure transport, quota on issuance only, ordinary
  query results and export capacity/artifact cleanup.
- `TestPostgresRoutesQueryOnlyAndEmptyPage`: no ticket dependencies or export
  routes, canonical empty array/page links.
- `TestPostgresRoutesCallbackFailureAndCancellation`: panic/Goexit containment at
  Authority, Issue, Redeem and WithLocale, redaction, the manager-derived download
  timeout, cancellation identity, capacity and cleanup.
- `TestPostgresRoutesCancellationRetainsCallbackOwnership`: canceled HTTP requests
  retain an uncooperative redeemer until it actually exits.

Consumer and Go-way negatives reject wrong authority result, guard-option actor
and ticket-issuer actor. Strict TypeScript covers the framework request, exact
expiry response, complete download query, format rejection and file ownership.
The runtime client verifies DTO encoding and actual HTTP denial responses.

## Acceptance commands and results

Native tools reused the warm Go cache with `GOFLAGS=-trimpath`; no dependency was
installed. The workspace had approximately 208 GiB free before builds. Cache
access and local integration connections used approved sandbox escalation.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./datatable` | PASS |
| Consumer: `go vet . ./reporting ./clientcontracts` | PASS |
| Go-way: `go vet .` | PASS |
| `go test ./datatable` | PASS; PostgreSQL opt-in cases skipped in this environment-free package pass |
| `FOUNDRY_TEST_POSTGRES_REQUIRED=1 go test -count=1 -timeout=20m ./datatable -run '^TestPostgresRoutes'` | Segmented PASS: lifecycle assertion corrected and rerun below; all other cases passed in this run |
| Same PostgreSQL environment, `go test -count=1 -timeout=20m ./datatable -run '^TestPostgresRoutesTicketLifecycle$'` | PASS |
| `make generate-check` | PASS across all 15 configured declaration roots |
| `make publish-client-contracts` | PASS; current cohort only |
| Consumer: `go test -count=1 ./clientcontracts` | PASS |
| `make typescript-check` with the requested Node/TypeScript paths | PASS, including reporting/table-binding strict typing, runtime HTTP/WebSocket and React/Vue adapters |
| Go-way: `go test -count=1 ./guideadapters ./guideexamples` | PASS |
| Consumer and Go-way: `go test -count=1 . -run '^TestInvalidPublicContractsFailCompilation/datatable_routes_'` | PASS in both modules |
| `make release-tools-check` | PASS; release Go vet/tests and 38 Python tests |
| `python3 tools/goway_catalogue.py` | PASS; 1,058 rows |
| `make docs-check` | PASS |
| `git diff --check` | PASS |

The PostgreSQL URL was read without logging or shell-sourcing from the existing
private `Foundry-Go/.env.test`, then passed only through the child environment.
Tests used existing service configuration and retained isolated schemas; no
reset, drop, truncate or duplicate server was used. The TypeScript environment was
`FOUNDRY_TEST_NODE=$(command -v node)` and
`FOUNDRY_TEST_TYPESCRIPT=/Users/weiloon/Projects/rust-go/Foundry-Go/tools/typescript/node_modules/typescript/lib/tsc.js`.

Initial corrections: generation needed a bootstrap because the command imports
`datatable` before its new descriptors exist. The existing generator was built
with a temporary Go overlay omitting only the new route implementation, then run
against the complete source. No generated file was handwritten. The first test
compile required `FoundryIdentity` on the test actor. The first PostgreSQL run
caught an assertion expecting a string total; canonical numbered-page JSON uses
numeric totals. Only the affected lifecycle test was rerun after correction.

## Design review and limits

No behavioral deviation from the approved Part B design. Portal-neutral filenames
are `routes.go` and `routes_transport.go`. Routes use the shipped Part A constructors
and binder. Existing HTTP callback isolation contains application callbacks and
retains their lifetime; the manager isolates pre-issuance authorization and owns
deferred export work. Query-error mapping traverses only framework-owned URL
codec errors. Application callback errors retain the existing HTTP classifier.

Credentials remain application-owned; these tests exercise the adapter's handling
of redemption errors, not a new token implementation. Custom protocols retain
Part A. Framework format fields carry no application translation label.

Full `make verify`, race suites, application migrations and release tagging were
outside the requested gate and were not run. No performance claim is made.

Changed source/contract fingerprint (sorted relative path, NUL, bytes, NUL;
`.go`, `.ts`, `.mjs`, `.py`, `.tsv`, `.json`; excludes documentation):
`ea5bd8ef468fa6b1ed71d25e85d8f5622920574a37b6db48d2c7da37e40aa01b`.
