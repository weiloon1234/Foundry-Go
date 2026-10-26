# Frozen legacy oracles

The 909 observations published at commit `54508185` are historical evidence from
real legacy execution, including PostgreSQL hydration and aggregate results.
`records.json` (including its source hashes), `comparisons.json` and `state.json` retain the
original provenance and publication receipt. Do not regenerate them from ORM output.

Stage 8c removes the generated legacy declarations and recorder. Current API tests
replay the same SQL, bindings, hydration, callback and cursor observations through
`tests/oracle`. The integrity guard rejects changed observations or missing
provenance. Restoring a capture command requires the approved historical checkout
and a separate lead decision; this checkout has no recording path.

Snapshots preserve kinds, string boundaries, nil/empty states, pointer presence,
field names and scalar internals. Model identities and pointer addresses are
normalized; cursor wire tokens retain their recorded identities. Existing
normalization of randomized sensitive values remains unchanged.

Replay determines expected records independently from what a test emits. Normal
`-run` and `-skip` flags select tests and subtests, and all selected occurrences are
required, including tests that emit zero observations. PostgreSQL entry points in
the reviewed census are required only when `FOUNDRY_TEST_POSTGRES_URL` is nonempty
or `FOUNDRY_TEST_POSTGRES_REQUIRED=1`. Disabled backend tests are deselected; ordinary
tests remain strict. `-list` only inspects tests.

The fixture TestMain owns replay completeness. Use the Makefile's shared source
fingerprint or `go test -count=1` for direct fixture runs, because the consumer reads
records in its sibling goway module. Checkout and packaged consumer layouts use
the same directory lookup. `FOUNDRY_ORACLE_READ` overrides the observation directory,
not the reviewed backend census.

The import guard parses every Go import, including escaped paths and build-tagged
files. Its sole exception is `oldmodels/cursor_identity.go`, a handwritten ORM
model at the original package path and type name. Only `reads_golden_test.go`
imports that fixture; the cursor transport and statement tests share its typed
alias. Keeping this identity proves a real consumer migration preserves existing
cursor tokens. No generated code or legacy query declarations remain in that
package. Recording-file and migration-before exceptions are retired. Migration
"before" snippets are documentation-only; "after" snippets stay compiled.
Legacy compile-fail programs are immutable text-only catalogue inputs and are
excluded from compilation by their source-hash retirement registry. Legacy
footprint numbers are frozen measurements, not new performance claims or ceilings.

