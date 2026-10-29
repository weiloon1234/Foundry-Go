# Consumer codemod

Type-aware v0.1 to v0.2 validation migration provides a dry-run inventory and
explicit apply mode, preserves callback receiver capture, and reports manual
adapters and generated-file regeneration. Release changelog entries belong to
the integration step.

Run from a consumer with its **current v0.1 dependency** still installed:

```sh
go tool foundry codemod ./...
go tool foundry codemod --apply ./...
go tool foundry codemod --dir /path/to/consumer ./...
```

The tool binary must contain this subcommand. Build or install that tool separately
if the consumer's pinned tool version predates it; keep the consumer dependency at
v0.1 during migration. Upgrade to v0.2 and run `go tool foundry generate` after
applying handwritten changes. The rewritten source intentionally does not compile
against v0.1, so finish the dependency upgrade before running another inventory.

Dry run is the default. Each finding includes `file:line`, its status and its
migration. Apply refuses the entire plan if unresolved findings remain. Generated
headers and `*_foundry.gen.go` are left untouched and listed for regeneration.
Apply checks original file bytes and preserves permissions; each file replacement
is atomic. A filesystem failure can leave earlier files applied; the error reports
the number written. Keep the workspace idle during apply.

The loader follows `internal/noraw`'s standard-library export-data approach:
`go list -deps -export -compiled -test -json`, then `go/types`. This can compile
current dependency exports, but does not execute consumer tests. It includes active
build-tag files and test package variants, deduplicates identical edits and rejects
conflicting variants. Run separate inventories for other build configurations.
Selected source must be under `--dir`; cgo cache source outside that directory is
rejected. Load/type errors stop the operation before edits, without printing compiler
output that might contain private source literals.

`RuleSet.Rewrite` is the extension point for HTTP and ORM. The engine supplies AST,
type information and parents, collects findings, composes nested AST replacements,
and formats changed files with `go/format`. Validation matches the selected
`types.Func.Origin` against Field/Rule declaration objects from the consumer's
imported package. Consumer methods with matching names are untouched. Imports
retain their aliases; missing imports get fresh aliases. Dot or shadowed imports
are unresolved.

| v0.1 form | v0.2 form / coverage |
| --- | --- |
| `f.Rules(rs...)` | `validation.Rules(f, rs...)` |
| `f.Validate()` | `validation.ValidateField(f)` |
| `f.Name()` | `validation.FieldName(f)` |
| `f.Select(input)` | `validation.Select(f, input)` |
| `f.WithLabel(label)` | `validation.WithLabel(f, label)` |
| `f.WithLabelKey(key)` | `validation.WithLabelKey(f, key)` |
| `r.Validate()` | `validation.ValidateRule(r)` |
| `r.Description()` | `validation.DescribeRule(r)` |
| `r.Check(ctx, input, limits)` | `validation.Check(r, ctx, input, limits)` |
| `r.CheckProhibitions(ctx, input, limits)` | `validation.CheckProhibitions(r, ctx, input, limits)` |
| `r.WithMessage(message)` | `validation.WithMessage(r, message)` |
| Chained calls | Nested descriptor-first calls; a capture closure precedes argument calls/receives to retain receiver evaluation order |
| `validation.Field[T,V].Rules`, `validation.Rule[T].Check` | Explicitly instantiated package functions; all mapped methods supported |
| Method values, e.g. `next().Check` | Typed immediately invoked function captures the receiver once and returns a forwarding closure; variadic callbacks preserved |
| Pointer or promoted receivers | Unresolved; explicit dereference/snapshot or adapter needed |
| Multi-result argument expansion | Unresolved; explicit argument expansion needed |
| Unnameable/shadowed callback types | Unresolved |
| Interface assignments, calls, returns, composites, sends and generic constraints | Unresolved when a removed method supplies the requirement |
| Generated files | Regenerate after upgrading the dependency |

Interface inventory is static, not a whole-program proof. Dynamic assertions from
`any`, reflection, inactive build configurations, and downstream modules require
review. HTTP and ORM migrations are not registered yet. No dependency is added.

Tests use a frozen synthetic v0.1 validation package so removal of framework methods
does not invalidate the migration oracle. Golden pairs cover all 11 methods,
chaining, variadics, method expressions, parentheses, aliases, comments and unrelated
methods. Focused tests cover closure capture, refusal cases, generated files and
stale-input/apply behavior. The lead checkpoint passed `go build ./cmd/foundry`,
`go vet`, and `go test -run Codemod ./cmd/foundry`. It found extra blank lines after
method expressions in `TestValidationGolden`; setting only the closing bracket
then produced trailing commas and line breaks because the elements retained
positions from a separate parser input. The rule now copies matching original
brackets and elements together, and consistently anchors synthesized alias type
arguments. The unchanged golden awaits lead re-verification. No tests were run in the implementation lane.
