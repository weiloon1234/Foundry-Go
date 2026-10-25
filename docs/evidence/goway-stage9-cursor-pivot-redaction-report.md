# Stage 9 review 21: cursor and pivot redaction

Cursor tokens now use a private, non-generic, method-free state type over Go’s
standard `unique.Handle[string]`. It retains an opaque pointer and preserves
value equality across independent parses. `Token()` still returns the original transport token; parsing, validation and zero-cursor
behavior are unchanged. The initial fresh-pointer candidate failed the existing
ORM alias and HTTP codec equality tests; the final interned holder preserves that
contract without an application-owned registry or a new method.

Morph-pivot fixed values reuse the existing shared literal holder. Single and bulk pivot writers unwrap at the ordinary assignment
capture boundary, preserving the discriminator's stored type and binding.
No framework methods, surface entries, catalogue rows or budget ceilings changed.

Regressions cover `%v`, `%+v`, `%#v`, private and nested wrappers, pointers and
`fmt.Errorf`; ORM/legacy cursors, requests and page containers; both morph-pivot
directions; cursor token/key roundtrips; parameterized pivot filters; and single
and bulk pivot defaults. The bulk-default regression stops at draft inspection,
before any database operation.

The P3 is accepted as directed: `LIMIT`/`OFFSET`, identifiers and SQL shape are
structural metadata outside the redaction guarantee. The ORM README and query /
pagination guides state both the protected data categories and explicit accessor
exceptions. Model/page item data is not implicitly redacted.

## Main port map

The [cumulative main port map](goway-stage9-diagnostic-redaction-report.md#main-branch-port-map)
now includes `database/query/cursor_token.go`, its direct readers in
`database/query/cursor_boundary.go` and `database/query/cursor_validation.go`, and
`database/query/relation_morph.go`, `database/query/relation_through.go`,
`database/query/relation_write_keys.go`, `database/query/relation_pivot_sync.go`.
Main was inspected read-only at `10030ad4eaf6ecf6caf269dfb5e970b0816f366e`.

## Shared files

Runtime: `internal/querycore/cursor_contract.go`,
`database/query/relation_morph.go`, `database/query/relation_write_keys.go`,
`database/query/relation_pivot_sync.go`.
Tests: `database/orm/cursor_retention_test.go`,
`database/query/relation_pivot_redaction_test.go`.
Docs: `database/orm/README.md`, `docs/guides/model-queries.md`,
`docs/guides/model-pagination.md`, the cumulative port-map report, and this
report/evidence pair. Earlier accepted lane changes are preserved.

## Acceptance

Final-source acceptance uses `GOCACHE=/private/tmp/foundry-goway-codex-gocache-r9`,
`GOMAXPROCS=4 GOFLAGS=-p=4 GOWORK=off`.

| Command | Result |
| --- | --- |
| `go build ./...` | PASS |
| `go vet ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./http/pagination/...` | PASS |
| `go test -race ./database/orm/... ./database/query/... ./internal/querycore/... ./internal/ormcore/... ./http/pagination/...` | PASS |
| Fixture `go test -v . -run 'TestFootprintBudgets\|TestExtensionMatching\|TestZeroExtensionDescriptorExtraction'` | PASS |
| Fixture `go vet .` | PASS |
| `PYTHONDONTWRITEBYTECODE=1 python3 tools/goway_catalogue.py` | PASS (548 ported / 34 pending / 444 unaffected) |
| `make docs-check` | PASS |
| `gofmt -l` on the six changed Go sources; `git diff --check` | PASS |

All 129 probes passed with no growth against review 20: 128 exact matches;
`extension_slots_old` decreased from 9,730 to 9,729. `q_with` remains 1,211/1,211,
`aggregate_families` 4,365/4,369. Cursor probes’ function counts are unchanged.
Public-method snapshot, catalogue and footprint-registry hashes are unchanged.

The initial race run caught the fresh-pointer equality regression in
`TestPaginationContractBytesAndRedaction` and
`TestCursorQueryCodecPreservesTypedPosition`. Both passed with the interned holder,
then the full targeted race command passed. No test expectation was weakened.

No PostgreSQL was run. Generation, TypeScript and editor checks were not repeated:
this fix changes retained state, with no declaration or wire-contract change;
HTTP pagination contract tests passed. Root builds emitted only the existing
best-effort module-cache stat-write sandbox warning and exited successfully.

## Docs notes

Retained predicate literals, bound data values, assignment inputs, bounds, cursor
tokens, plans and pivot bindings are redacted from formatting and interpolated
errors, including private/nested wrappers. Structural query metadata is not secret.
Explicit accessors still disclose their data. Lead-owned docs were not edited.
No commit was made.
