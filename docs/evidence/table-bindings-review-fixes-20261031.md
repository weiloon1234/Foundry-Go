# Table binding review fixes

2026-10-31. Branch `lane/tablebind-fixes`, base `db7a461c274c1b0d9fa10fdfe363355b60bd62b0`.

## Changes

- TB1: `TablePageReceived<Row>` renders from the canonical numbered pagination
  template with a distinct internal row placeholder. Fixed metadata and links
  retain their concrete types when a table row shares those schema identities.
  The strict regression uses `NumberedMeta` for the first table and a different
  queried row for the second. Both operation responses and table outputs are
  assignable in both directions, without assertions.
- TB2: `http.InvalidEndpointOption(cause error) EndpointOption` carries construction
  failures through the existing sticky route declaration error. Safe context names
  the endpoint and datatable constructor, table, and invalid field where applicable.
  Tests check cause identity, undefined declarations, redaction, and preservation
  of earlier errors.
- TB3: The projection guide now describes explicit operation bindings and their
  schema dependencies. Metadata-only tables remain in the full manifest.
- TB4: The generated-configuration guide documents bare `optional`, every kind's
  `T | undefined` result, supplied-value validation, and fallback restrictions.

Files changed:

- Renderer and regression: `typescript/render_tables.go`, `typescript/table_page_test.go`.
- Construction errors: `datatable/endpoints.go`, `datatable/endpoints_test.go`,
  `http/options.go`, `http/endpoint_error.go`, `http/endpoint_error_test.go`.
- Documentation: `docs/guides/client-contracts.md`,
  `docs/guides/generated-configuration.md`, `CHANGELOG.md`, and this evidence file.
- Publication: `tests/fixtures/consumer/clientcontracts/testdata/current-contracts/provenance.json`.
  Existing generated client artifacts remain byte-for-byte unchanged.

## Verification

All commands used the native warm cache with `GOFLAGS=-trimpath`.
Strict TypeScript runs also set:

```sh
FOUNDRY_TEST_NODE=$(command -v node)
FOUNDRY_TEST_TYPESCRIPT=/Users/weiloon/Projects/rust-go/Foundry-Go/tools/typescript/node_modules/typescript/lib/tsc.js
```

| Command | Result |
| --- | --- |
| `go build ./...` | Passed |
| `go vet ./typescript ./datatable ./http` | Passed |
| Focused package command below | Passed all three packages, including strict TB1 regression |
| Public API guard command below | Passed |
| `make generate-check` | Passed all configured roots |
| `make publish-client-contracts` | Passed; only current-cohort provenance changed |
| Consumer: `go test -count=1 ./clientcontracts` | Passed |
| `make typescript-check` | Passed strict contracts, generic adapter, real clients, React and Vue |
| `make release-tools-check` | Passed release Go vet/tests and all 38 Python tests |
| `make docs-check` | Passed |
| `git diff --check` | Passed |

```sh
go test -count=1 -timeout=5m . -run '^TestConsumerAPI(MethodVisibility|Documentation|DSLGoDocSurface)$'
go test -count=1 -timeout=5m ./typescript ./datatable ./http -run '^(TestTable.*|TestInvalidEndpointOptionPreservesStickyCause|TestRouteFactoriesCannotSubstituteDeclaredTransport|TestSignedAuthenticatedModelBindingPreservesAllBoundaries)$'
```

The strict regression was also run with a temporary Go overlay restoring the
original `typescript/render_tables.go` from HEAD. It failed as expected with
TS2322: the second row replaced the fixed `meta` field. The same regression passed
with the corrected renderer. No source rollback or generated-file hand edit was used.

The changed Go source/test files, sorted by repository-relative path and hashed
as `path + NUL + contents + NUL`, have aggregate SHA-256:
`a21ee5b411812ead37a47f9383dd3496e491f0330172b9026596c260405dde26`.

Frozen cohorts, including `contracts-v0.3.0`, are unchanged. No full `make verify`,
race run, app edit, commit, push, merge, or release tag was performed.
