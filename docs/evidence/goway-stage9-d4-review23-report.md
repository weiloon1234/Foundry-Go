# Stage 9 D4 assertion-preservation follow-up

## Changes and sweep

The root assignment checks now pass the actual assignments into `Table.Insert`
and inspect the operation-local `orm.Write` in a registered Creating observer.
The observer deliberately aborts before SQL, and the test requires one callback,
one transaction begin and one rollback. No manually constructed model supplies
an expected input value. Coverage includes zero, empty string, explicit NULL,
unset versus omission, model-owned IDs, imported enum/natural-key values and
exact decimal inputs. The existing decimal codec/binding assertion remains.

The D4 sweep also restored the two business-helper assertions that had been
removed: `User.CurrentEmailDraft` and `Product.CreatorDraft`. Those shared-model
helpers still return legacy drafts; their original behavior stays covered until
lane C migrates their declarations. No edits were made to lane C's models/catalog.
The stored-snapshot change tests still deliberately compare stored values rather
than draft input. Read, pagination, write, observer and benchmark parity checks
were reviewed against HEAD; their behavioral assertions remain.

Real completion probes now require `Nickname.SetNull` and forbid `Email.SetNull`,
while requiring `Set` on both. The article Title handle and its completion/hover
probe again require “localized article heading shown to readers”. Generic handle
documentation remains checked as well.

The three blocked relation/through/aggregate oracle files remain byte-identical
to HEAD. No runtime API, compiler row, public method snapshot, footprint probe or
ceiling changed in this follow-up. Generated output, manifests and TypeScript
are unchanged. No commit was made.

## Acceptance

Targeted build, vet and race checks pass. Negative controls change age to 99,
creator ID to another owner-typed ID, and decimal input to zero; each fails at its
intended input assertion. Catalogue, generation-check (the Make prerequisite),
docs-check, gofmt and diff-check pass. No PostgreSQL was run.

All **496 real gopls probes pass across segmented runs** with approved v0.23.0.
The initial `make agent-smoke` ran every probe: 466 passed and 30 failed during
server cleanup (28 shutdown cancellations, two exit cancellations). A serial
retry passed 29; `typed-insert-select-returning` hit another shutdown timeout and
passed alone. Other lanes' compilers and gopls gates were running concurrently;
no deadline, cleanup check or assertion was relaxed. The literal Make invocation
failed; the completed segmented coverage is the final editor result. Exact
commands, logs and current source hashes are in `goway-stage9-d4-review23.json`.

Initial cleanup failures (all subsequently passed):

- `TestRealGoplsConsumer/typed-source-delete-natural-key`
- `TestRealGoplsConsumer/typed-source-update-key`
- `TestRealGoplsConsumer/typed-insert-select-values`
- `TestRealGoplsConsumer/typed-insert-select-returning`
- `TestRealGoplsConsumer/typed-insert-select-field`
- `TestRealGoplsConsumer/typed-per-model-update`
- `TestRealGoplsConsumer/typed-relation-write-limit`
- `TestRealGoplsConsumer/typed-transaction-lateral-join`
- `TestRealGoplsConsumer/typed-transaction-correlated-lock`
- `TestRealGoplsConsumer/typed-case-branches`
- `TestRealGoplsConsumer/typed-parameter`
- `TestRealGoplsConsumer/typed-aggregate-filter`
- `TestRealGoplsConsumer/typed-lease-binding`
- `TestRealGoplsConsumer/recursive-working-fields`
- `TestRealGoplsConsumer/extension-files-in-transaction`
- `TestRealGoplsConsumer/orm-extension-owner`
- `TestRealGoplsConsumer/generated-http-query-enum-slice`
- `TestRealGoplsConsumer/generated-http-dto-decode`
- `TestRealGoplsConsumer/generated-http-path-descriptor`
- `TestRealGoplsConsumer/generated-http-query-descriptor`
- `TestRealGoplsConsumer/generated-http-float-descriptor`
- `TestRealGoplsConsumer/typed-http-endpoint-handler`
- `TestRealGoplsConsumer/typed-http-proxy-header`
- `TestRealGoplsConsumer/typed-http-proxy`
- `TestRealGoplsConsumer/typed-http-cors`
- `TestRealGoplsConsumer/validation-password-value`
- `TestRealGoplsConsumer/validation-batch-model`
- `TestRealGoplsConsumer/typed-validation-prefix`
- `TestRealGoplsConsumer/typed-http-signed-route`
- `TestRealGoplsConsumer/typed-transaction-projection`

## Shared files and Docs notes

Shared files changed here: `internal/agent/gopls_acceptance_test.go` and
`internal/agent/field_documentation_acceptance_test.go`. Other edits are the root
consumer assignment tests, their transaction-only driver helper, and the article
Title handle comment. Lead-owned documentation is untouched. Keep the two
business-helper assertions during their shared-owner migration; keep lane A's
custom loader-limits work separate.
