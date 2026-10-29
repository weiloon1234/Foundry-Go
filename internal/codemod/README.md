# Consumer codemod

Type-aware v0.1 to v0.2 validation, HTTP, and ORM migration provides a dry-run inventory and
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

`RuleSet.Rewrite` is the extension point for migration families. `HTTP` is a
separate rule set registered by `Inventory`, including when the caller supplies
`Validation`. Passing `HTTP{}` explicitly does not register it twice. The engine supplies AST,
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
review. Validation, HTTP, and ORM migrations are registered by default. No dependency is added.

Tests use a frozen synthetic v0.1 validation package so removal of framework methods
does not invalidate the migration oracle. Golden pairs cover all 11 methods,
chaining, variadics, method expressions, parentheses, aliases, comments and unrelated
methods. Focused tests cover closure capture, refusal cases, generated files and
stale-input/apply behavior. The lead reports that `go vet` and
`go test ./internal/codemod/...` passed at the checkpoint before Amendment A1.
The A1 changes below have not been tested in this implementation lane.

## HTTP coverage

[http_methods.go](http_methods.go) records the appendix's 156 removed methods.
The HTTP rule matches each selected method's origin against its receiver's
method declaration in the consumer dependency. It does not import the framework's
HTTP implementation. Package-function rewrites also require declaration identity.
Scope methods, payload methods, endpoint `Handle` and `URL`, and pagination
configuration methods remain unchanged.

| v0.1 form | Coverage | Result or manual decision |
| --- | --- | --- |
| Endpoint, authenticated, optional, signed, and idempotent descriptor calls | Handled | Exact family-specific package helpers from the approved appendix |
| Route, authenticated route, optional route, and signed route calls | Handled | Route helpers, including raw handlers, signing, and URL functions |
| Path, query, body, response, and multipart descriptor calls | Handled | Descriptor-specific helpers |
| Pagination numbered, simple, cursor, and authenticated methods | Handled | Shared pagination helpers, resolved through the underlying receiver of exported aliases |
| Modelbinding endpoint, authenticated, idempotent, and resolver methods | Handled | `AuthorizeResource`, `DescribeEndpoint`, and the corresponding family helpers |
| `DefineEndpoint(...).Within(s).WithTimeout(d)` | Handled | `DefineEndpoint(..., Within(s), Timeout(d))` in declaration order |
| Adjacent common options on route and pagination constructors | Handled | Trailing options when constructor and option arguments have no calls or receives |
| Known authentication constructors followed by actor options | Handled | Trailing guard or actor options; `ActorMiddleware[M]` retains the concrete actor type |
| Saved values with plain configuration options | Handled | The family-specific `Configure` helper, with the receiver evaluated once |
| Signed settings | Handled | `ConfigureSigned` and its route and actor counterparts |
| Typed hooks mixed with options | Handled | Nested helper calls; options never cross a typed hook or authentication transition |
| Variadic methods and option arguments | Handled | Preserve slice expansion inside the helper or option call |
| Direct helper method expressions | Handled | Explicit generic arguments, including receiver aliases |
| Direct helper method values and calls with argument effects | Handled when types can be named | Reuse the typed capture closure so the receiver is evaluated once before argument effects |
| Configuration method values and expressions | Unresolved | Require a typed closure that converts arguments to options |
| Option arguments with calls or receives | Unresolved | Review side effects and evaluation order before collecting options |
| Constructor arguments with calls or receives | Handled without flattening | Keep the constructor inside the configure call |
| `Bind` on ordinary and signed endpoints | Handled | `ForBinding` or `ForSignedBinding` |
| `BindAuthenticated` on required, optional, signed-required, and signed-optional endpoints | Handled | The corresponding explicit binding adapter, retaining optional actor types |
| Stored binding or replay constructor functions | Unresolved | Supply a typed adapter before passing or storing the constructor |
| Custom or indirect binding transports | Unresolved | Choose staged binding or explicit `AfterValidation` timing |
| `Idempotent(Bind(e, r), s, d)` | Handled | `BindIdempotent(http.Idempotent(e, s, d), r)` |
| `Idempotent(Bind(se, r), s, d)` | Handled | `BindIdempotent(http.IdempotentSigned(se, s, d), r)` |
| `IdempotentAuthenticated(BindAuthenticated(a, r), s, d)` | Handled | `BindAuthenticatedIdempotent(http.IdempotentAuthenticated(a, s, d), r)` |
| Bound resource policies and replay headers | Handled | `AuthorizeIdempotentResource` or `AuthorizeAuthenticatedIdempotentResource`, followed by the existing replay header helper |
| Adjacent local `bound := Bind(...); operation := Idempotent(bound, ...)` declarations | Handled | Remove a replay-only bound declaration; retain an ordinary bound declaration when shared |
| Side effects or potentially panicking arguments in a replay initializer with a single fresh local name | Handled | Capture endpoint, resolver, policies, store, and definition once in their original order before constructing replay |
| Optional-actor and signed-actor replay, including signed optional actors | Unresolved | Previously rejected at registration; A1 adds no replay transition for these families |
| Custom replay transports | Unresolved | Previously rejected at registration; expose a supported concrete replay descriptor explicitly |
| Nonadjacent, indirect, address-taken, or ambiguous saved replay declarations | Unresolved | Review declaration ownership and snapshots |
| Observable replay argument evaluation outside a supported local declaration | Unresolved | Introduce local captures before rearranging constructors |
| Explicit replay constructor type arguments | Handled | Infer the new bind arguments from the concrete endpoint and resolver; remove imports used only by discarded type arguments |
| Old transport interface type references | Unresolved | Select the binding capability or handler contract |
| Interface assignments, arguments, returns, composites, sends, and constraints relying on removed methods | Unresolved | Supply an explicit consumer adapter |
| Pointer or promoted receivers, unnameable callback types, dot or shadowed imports, multi-result arguments | Unresolved | Make ownership, imports, or argument expansion explicit |
| Generated files | Regenerate | Engine skips both generated headers and `*_foundry.gen.go` |
| New helper-only forms, retained constructors, and retained terminals | No rewrite needed | Existing spelling remains valid; constructor options are added only when collecting a chain |

The HTTP goldens include a frozen, simplified v0.1 package for every mapped
receiver family. They cover destination names independently of the runtime table;
they do not establish v0.2 signature or runtime parity. Focused fixtures cover
chain order, saved declarations, actor options, aliases, binding families, receiver
capture, unrelated objects, and unresolved cases. A1 adds replay golden pairs for
ordinary, signed, and authenticated endpoints, policies, headers, nil callbacks,
shared declarations, and ordered captures. Focused tests cover rejected replay
families, import cleanup, receiver identity, and generated-file skipping. No tests,
builds, or other verification commands were run for A1. Changed Go files were
formatted.

Amendment A1 selects explicit replay binding. HTTP's `Idempotent`,
`IdempotentSigned`, and `IdempotentAuthenticated` constructors remain unchanged.
The codemod removes modelbinding's old replay transitions without inventing an
optional or signed-actor replay capability. It retains the source endpoint as the
owner of signing and authentication.

For a local replay declaration, policies and headers become ordinary assignments:

```go
operation := modelbinding.BindIdempotent(http.Idempotent(e, store, definition), resolver)
operation = modelbinding.AuthorizeIdempotentResource(operation, policy)
operation = modelbinding.IdempotentHeaders(operation, headers)
```

Policies keep their original order and replacement behavior. A supplied nil policy
or header remains explicit. Shared declarations reuse captured endpoint, resolver,
and policy values; callback bodies are not copied. The supported saved form uses
adjacent single-name short declarations in the same block. Other saved forms stay
unresolved rather than tracing arbitrary assignments or changing control flow.
New local names avoid source identifiers and other generated names. Shadowing
that would change a policy or header reference prevents statement expansion.



## ORM coverage (Design 2)

ORM resolves methods and receiver families from the consumer's v0.1 export data.
It does not import the framework ORM implementation. The 397 operator mappings
across 33 families follow the receiver-first table in `fp-orm-proposal.md`.
Aliases use their declared receiver family, preserving key and input roles.
Nested rewrites compose without flattening query chains.

| Form | Coverage | Result or reason |
| --- | --- | --- |
| Column operators across all 33 mapped families | Handled | Per-family helpers, including bounds and variadic expansion |
| Query terminals | Handled | Context and executor precede the query; remaining arguments retain order |
| Query locking | Handled | `Lock`, `ForShare`, `ForNoKeyUpdate`, `ForKeyShare` |
| Locked query terminals | Handled | `AllLocked`, `FirstLocked`, `RequireLocked`, `EachLocked` |
| Direct table terminals | Handled | Table follows context and executor; `Require` becomes `RequireKey` |
| One resolved value embedding of Table | Handled for calls without argument effects | Select the actual embedded field, including aliases; never guess its name |
| Stable table Update/Delete/ForceDelete calls | Handled | Use the resolved Table's Query and Key; retain the soft-delete argument position |
| Scoped writes with effectful receivers/arguments or unstable field paths | Unresolved | Require explicit captures before expanding the table into Query and Key |
| Package-level rows/expr/conflict and other sealed column APIs | Handled | Insert per-family adapters only at parameters with imported sealed column constraints |
| Concrete-key APIs and column constructors | Unchanged | Concrete parameters do not acquire adapters |
| Operator and locking method expressions | Handled when type arguments can be named | Explicit generic helper instantiation |
| Operator and terminal method values; calls with argument effects | Handled when types can be named | Typed closure captures the receiver once before argument effects |
| Draft helper setter callbacks | Handled when types can be named | Rewrite the method value into a typed captured callback |
| Terminal method expressions | Handled when types can be named | Typed receiver-first forwarding closure for the changed argument order |
| Pointer/promoted column receivers and deeper or pointer table embeddings | Unresolved | Resolve field paths and snapshot ownership explicitly |
| Generic helpers instantiated with affected handles and old method-based interfaces | Unresolved | Supply typed operator callbacks or specialize the column family |
| Unknown sealed column families/roles, explicit generic arguments fixing a column type, expanded sealed-column slices | Unresolved | Review concrete adapter types and per-element conversion |
| Dot/shadowed imports, unnameable callback types, multi-result arguments | Unresolved | Make types, imports, or argument expansion explicit |
| Query chains, locked wait policies, Table.Query/Key, rows/expr methods | Unchanged | These retained methods are outside the removed-method mapping |
| Unrelated objects with matching spellings | Unchanged | Match imported declaration objects, not selector names |
| Generated files | Regenerate | Shared engine skips generated headers and `*_foundry.gen.go` |

Frozen v0.1 goldens cover all 397 operator destinations independently of the
runtime lookup table, plus terminals, locking, table embedding, scoped writes,
input/stored types, and sealed adapters. Focused tests cover captures, unrelated
objects, generic/interface boundaries, and generated files. The loader fixture
also covers ORM registration and adding an import in a consumer file. Simplified
frozen signatures do not establish v0.2 runtime or signature parity. The lead
runs verification; this slice was formatted without running tests or builds.
