# Model conversion

`Analyze(ctx, directory, patterns)` type-loads packages through the existing
analysis loader. It returns per-model refusals, option counts and owned staged
file bytes; it does not invoke generation or write consumer sources. Applications
must already type-check. Test-file models are outside this API's scope.
`Plan.OutsideDirective` reports generic models and ORM-owner declarations separately
from refusals: these declarations remain handwritten under decisions 21/22.

`Prove(ctx, plan, ProofOptions{Generator, FrameworkDirectory, ScratchDirectory})`
copies the module's Go sources into scratch, captures ORM declarations in a
package-local snapshot test, applies the staged files, invokes `Generator.Generate`,
and captures them again. It compares every table in the package, including
previously converted tables, and rejects new, unproved table variables. Named
behavior declarations and extracted function bodies must match their retained
source. The supplied generator operates only on the scratch package.

`Verified.Files()` returns independent copies. `Verified.Publish(ctx, Publisher)`
checks source freshness and hands the verified batch to the integrating command's
publisher. The publisher must perform the existing ownership, locking, guarded
write and recovery protocol for the entire batch, including handwritten sources.
A `Plan` cannot publish. Proof is conservative and rejects the whole package when
any model is refused or different; it never publishes a partially proved package.

The private typed metadata schema and property comparison live under
`database/orm/internal/declarationsnapshot`. `orm.DeclarationSnapshot` is the
small read-only bridge needed by separately compiled consumer packages. Extension
handles supply their descriptions; the snapshot does not infer stored names from
model field spelling. A capability-gated ORM comparison bridge lets the converter
use that private schema without exposing raw runtime internals.

Factory symbols and codec entry points are metadata, not a proof of arbitrary
function semantics. Source preservation additionally covers handwritten factories,
mutators and codecs. This does not verify SQL execution, database DDL, data,
external services, package initialization side effects, ambient state, or dynamic
configuration. Snapshot subprocesses execute package initializers. The application
must be suitable for metadata inspection without external I/O.

The proof copy excludes test sources, symlinks, hidden directories, deployment
files and private configuration. Packages needing excluded or embedded resources
fail closed. The command supplies PublicationValidator, which checks the exact same
ownership plan as publication before proof accepts changed generated output.
Without that validator, changes to pre-existing generated source fail closed.
Source filenames alone never establish ownership. New generated output is
returned to the publisher; the converter never writes the original package.

The target framework must understand generated model tags through the shared
model-tag parser and its storage-only view. The converter does not duplicate that
grammar. Read-only recognition is not a claim that a generated conversion has
passed the runtime proof.

## Migration lifetime and removal

Everything in this package exists only for `foundry models convert`. Delete it
in the release after every consumer has converted. Delete
`database/orm/declaration_snapshot.go`, `declaration_snapshot_capture.go`,
`declaration_snapshot_test.go` and `database/orm/internal/declarationsnapshot/`.
In the eight existing ORM files below, delete the lines between (and including)
`// BEGIN foundry models convert` and `// END foundry models convert`, then gofmt.
The command integration must delete its conversion entry point at the same time.
No permanent runtime owner may depend on these types or functions.

| Existing ORM file | Declaration-time work added | Query-path work added |
| --- | --- | --- |
| `declaration.go` | Embeds private capture records in table, column and option; copies factory names per option and two setting flags per table | None |
| `column.go` | Records codec function symbols while binding; records mutator symbol while installing a mutator (nullable binding replaces its wrapper capture with the underlying codec) | None |
| `input_binding.go` | Records the selected input transform symbol when binding required/nullable input columns | None |
| `input_key.go` | Records input-key transform symbol when binding | None |
| `mutator.go` | Records conventional method symbol when binding; keeps it separate from the runtime adapter symbol | None |
| `global_scope.go` | Captures scope factory symbol when constructing its option | None |
| `retrieval.go` | Captures retrieval factory symbol when constructing its option | None |
| `write_draft.go` | Captures hook factory symbol when constructing its option | None |

The retained records add metadata memory per declaration. Symbol capture uses
reflection and runtime symbol lookup; codec capture allocates a small slice of
function descriptions. No added operation is in SQL compilation, query execution,
hydration, writes, or callback invocation. Snapshot traversal itself runs only when
the conversion bridge is explicitly called. These are structural cost statements,
not benchmark claims.

The proof checks exact table variable types, embedded base, member names and types,
key capabilities, and retained table method signatures. Under decision 17, members
are compared by name, independently of order; ordered model columns and slots
remain part of the runtime snapshot. Unconventional table types receive explicit
rename instructions. A field that collides with a promoted Table method retains its existing member through
`member=Name`; other member renames remain refused. Identity variables use the conventional table-variable first word plus Identity
(Users → usersIdentity, APIKeys → apiKeysIdentity). Reference discovery includes package
test variants and, for exported identities, the module's callers. A differently named identity referenced
outside the removed identity block is refused with all source locations and the
required conventional name. References to the conventional name remain unchanged.
The snapshot reads identity storage names through `FoundryReference`, so a generated
conventional name can replace an unreferenced old name without affecting equivalence. References in unrelated
modules outside this source workspace are not discoverable by this API.

One, many, through and aggregate-value relation constructors are recognized and
inferred from their model field types. Their typed targets/pivots and ordered slots
remain covered by the snapshot and table-contract proof. Bare `orm.Table[M,K]`
declarations are counted and refused: replacing them with a generated named table changes their Go contract and
requires a consumer call-site migration before this equivalence proof can pass.
