# Model, enum, projection and transport generation

Handwritten Go declarations own persistence names, transport fields and value types. Foundry generates documented, ordinary Go code in the same package. The [consumer models](../../tests/fixtures/consumer/models/models.go) and [consumer tests](../../tests/fixtures/consumer/generated_test.go) are compiling examples of the current API.

The current generator emits executable [typed model reads](model-queries.md) and [writes](model-writes.md), complete row decoders, typed field codecs and mutation drafts. [Typed relations](model-relations.md) bind ordinary Go key declarations to generated slots and metadata, including concrete many-to-many pivots. [Automatic field mutators](model-mutators.md) connect typed handwritten methods to the shared write runtime. [Explicit getters](model-accessors.md) retain stored fields and share automatic getter/mutator field documentation. [Typed hooks and observers](model-hooks.md) integrate normal writes with transaction-bound lifecycle behavior; remaining integrations follow the milestone tracker.

[Declared projections](model-projections.md) use `//foundry:projection` on separate result structs. Generated selection types check source ownership, destination values and nullability; complete projection decoders select only declared expressions. Projections share the package graph and publication pipeline below.

[Typed joins](model-joins.md) use generated `ModelFieldsAt` and `ModelNullableFieldsAt` accessors. They retain field operators while binding alias/join ownership and outer nullability. Existing `ModelFieldSet` names alias the base-model instantiation of the generated scoped field set. Projection builders infer their source scope and expose typed per-field selection methods.

[Derived projection sources](model-subqueries.md) receive matching `ResultFieldsAt` and `ResultNullableFieldsAt` accessors using record-owned scopes. Their result types, mapped column names, nullable states and enum codecs come from the same handwritten projection fields and shared field generator. The original `ResultFields()` accessor remains the destination mapping descriptor set.

## Commands and ownership

[Client-contract publication](client-contracts.md#ownership-checking-and-recovery)
reuses this publisher and recovery command in a separate client output directory.
Go and client ownership manifests cannot share a directory.

Run commands with the native Go toolchain. From the framework repository:

```sh
go run ./cmd/foundry generate --recursive --dir tests/fixtures/consumer
go run ./cmd/foundry generate --recursive --check --dir tests/fixtures/consumer
```

`make generate` and `make generate-check` run these commands for every generation directory, adding `--field-docs` because the repository maintains handwritten field notices. An installed `foundry` command accepts the same arguments from a consumer workspace. Omit `--recursive` to target one existing Go package; include it to select the Go packages matched by `./...` below `--dir`. Nested modules, vendor, testdata, `node_modules`, hidden/underscore directories and go.mod `ignore` directories remain separate scopes. Module resolution is read-only and honors the consumer's module/workspace selection; a module with `vendor/modules.txt`, or an explicit `-mod` in `GOFLAGS`, keeps Go's own module mode instead of `-mod=readonly`.

Packages that use cgo or are excluded by this platform's build constraints are skipped when they contain no Foundry declarations. A Foundry declaration in a cgo file, or in a file excluded by build constraints, fails with that file's path instead of treating its owned output as obsolete: generated output has no build constraints, so declarations must live in unconstrained files of pure Go packages.

`foundry generate` compares the framework version it was built from with the version the consumer module selects. A known different release fails before analysis; run the module-pinned `go tool foundry` (see [team adoption](team-adoption.md)). Development builds and local `replace` directories cannot be compared and are accepted. `foundry doctor` reports the same comparison as its `tool-version` check.

Diagnostics name module-relative source paths such as `models/user.go:12:6`. A failed `--check` lists every stale target with its reason: `missing`, `obsolete`, `content differs`, `comments differ`, `formatting differs`, `managed field notes differ` or `ownership manifest differs`. Generated headers name their declaration's source file without a line number, so adding lines above a declaration does not rewrite other generated files.

Packages are processed in dependency order; independent packages are analyzed concurrently with a bounded worker count, and errors are reported in the same dependency order as a sequential run. Only dependencies outside the selection are compiled for export data; selected packages are type-checked from source once for declarations and once as the complete overlay, and packages without Foundry directives skip the declaration pass. Generated dependencies are shared as checked Go packages in memory, so a fresh checkout can contain handwritten methods that return generated types from other selected packages. Every selected package is validated before any output is published. Import cycles, duplicate model tables across the selected graph, invalid dependents, stale files, and concurrent package/module-file changes fail generation. Dependencies outside the selected tree must already compile; select the appropriate consumer module root when those dependencies also need generation.

Generation first analyzes model, enum, projection, HTTP path/query and JSON DTO declarations and the symbols needed to understand them. Unrelated business methods, aliases, constants and variables can refer to absent generated declarations. Ignored model fields are excluded only from this initial analysis. Generation then type-checks the complete original package against generated source in memory; a broken method or invalid unrelated declaration still fails before publication. Enum aliases, inherited constants and `iota` groups retain their Go types. Declaration dependencies must be understandable before generation; declarations in cgo packages and generic model/enum/projection declarations are unsupported.

A generated package-level symbol or method that a handwritten declaration already uses fails with both positions, for example `models/user.go:40:6: UserFields is declared by handwritten code, but the Foundry declaration at models/user.go:12:6 generates it`. Two declarations that would generate the same helper are reported the same way.

Commit the generated `*_foundry.gen.go` files and `.foundry-gen.json` alongside the handwritten source. The manifest records content hashes and owns deletion of obsolete files. Do not hand-edit generated files. Unknown, edited, orphaned, or non-regular output files are rejected rather than overwritten. Missing owned files can be regenerated. `--check` does not publish or rewrite project files, and unchanged generation leaves file modification times alone.

Publication rechecks source/target state, stages synced output and backups, records a recovery journal, replaces individual files by atomic rename, and writes ownership last. Ordinary failures and cancellation roll back applied changes. File permissions are preserved. If an editor changes a published file during failure handling, rollback retains the journal and refuses to overwrite that edit.

By default generation never edits handwritten files. `foundry generate --field-docs` (or `generate.Options.FieldDocumentation`) opts into clearly marked [getter/mutator notices](model-accessors.md#notices-beside-the-actual-field) in handwritten model source. Without the flag, existing notices are left exactly as they are, neither refreshed nor removed, and `--check` ignores them. The same notices always appear on the generated query fields and draft methods. Opted-in notices are generated documentation, not ownership of the entire source file. They share the publication transaction and stale-output checks with generated files. Source notices are classified separately in the recovery journal; both normal publication and recovery verify that managed documentation changes preserve handwritten Go code and user comments. Opted-in regeneration can apply standard Go formatting to files whose notices change.

Field-notice publication retains an immutable new-source proof, so recovery can
survive another interruption after restoring an old handwritten file. Older
version 4 journals remain readable: recovery preserves validated new bytes before
restoration, or leaves an already-restored exact old target untouched when an old
writer consumed the new bytes. Changed targets and invalid proofs remain errors.


On the implemented Unix advisory-lock platforms, the next ordinary generation automatically recovers an interrupted publication. `foundry generate --recover --dir generation-root` performs recovery alone, using the scope recorded in the journal; omit `--recursive` for this command. Recovery keeps a completely published set or restores an incomplete set, including deleted and newly created outputs. It verifies hashes, file types, permissions and backups before restoration, and refuses to run while another publisher holds the process guard. `--check` never performs recovery or creates staging/guards.

Recursive publication uses one journal at the selected root and guards every participating package. Recovery makes one decision across that whole graph, including when one package had already finished publication before the interruption. A child cannot generate or recover independently while it belongs to a pending ancestor journal. Relative target paths are checked against recorded package directories, and filesystem operations use Go's `os.Root` confinement. Ordinary per-package manifests still own generated files, so later targeted generation remains possible after the graph publication finishes.

The advisory-lock adapter covers Linux, macOS, FreeBSD, NetBSD, OpenBSD and DragonFly; runtime verification covers Linux and native macOS. Other platforms use an exclusive directory guard and require manual inspection after a crash. Keep `.foundry-generate.guard` in place even when idle: deleting its inode while another process holds it would break locking. Ignore that guard and `.foundry-generate.lock/` in version control. Finished staging is moved to a unique `.foundry-generate.finished-*` directory before cleanup; a crash during cleanup may leave that completed staging for later removal. Do not delete retained journal/backups after a recovery error until their changes have been inspected. Individual file replacement and recovery do not make the whole output set one atomic filesystem transaction.

## Model declarations

Attach `//foundry:model table=users` to an exported defined struct. The default primary field is `ID model.ID[User]`. For a natural scalar key, declare `primary=Code` (as the consumer's `Country` does). A model-owned UUID primary key must belong to its own model even when `primary` is explicit.

Persisted fields must be exported and non-embedded. Column names default to snake case; declare `foundry:"column=email_address"` once to override one. Use `foundry:"-"` to exclude a field. Unknown options, malformed/duplicate tags, duplicate columns/tables, missing primary fields, unsupported types, and conflicting generated symbols fail generation with source positions.

A persisted `CreatedAt`/`UpdatedAt` pair enables [managed timestamps](model-timestamps.md) by convention. Each field must be a non-null, non-primary `time.Time` or `temporal.DateTime`. `timestamps=true` requires a valid pair; `timestamps=false` treats the fields as ordinary values. Managed notices share the existing field-documentation publication path and disappear on opt-out without removing handwritten comments or getter/setter notices.

Use `foundry:"default=database"` when a non-nullable field may be omitted on creation because its migration defines a default or identity. Combine it with a column mapping using a comma. The model records permission to omit the field; the SQL expression stays in the migration. Omitted model-owned UUID primary keys are generated by Foundry unless this marker delegates the default to the database.

Supported field types include booleans, strings, ordinary signed/unsigned integers, floats, named scalar types, `model.ID[M]`, `decimal.Decimal`, `time.Time`, Foundry temporal values, `value.JSON[Payload]`, and `value.Nullable[T]` of a supported type. Aliases remain visible in generated signatures. [Typed JSON payloads](json-models.md) can contain structs, maps, slices and explicit dynamic values. Direct persisted pointers, arbitrary structs/interfaces, slices/maps, nested nullable wrappers, and embedded model fields require later codec/extension work; they are not silently coerced. The [database codec guide](database-codecs.md) describes their SQL contracts.

Fields of type `relation.One[T]`, `relation.Many[T]` or `relation.Through[T, P]` declare non-persisted relationship slots. They cannot use column/default tags and do not receive SQL columns or draft setters. The model supplies `DefineRelations() ItsRelationSet` with typed key mappings. Targets and pivots must be concrete generated models; pointer and non-model arguments fail generation/type checking. Relation generation participates in the same complete-package overlay and publication transaction.

Fields of type `translations.Text`, `attachments.One[M]`, `attachments.Many[M]` or `metadata.Value[V]` declare [model extension slots](model-extension-slots.md): translated text, attachments and typed metadata stored in shared framework tables. They accept only a `foundry:"name=stored_name"` tag and receive no SQL columns or draft setters. The optional `DefineExtensions() ItsExtensionSet` supplies their policy, and the `extension_owner=name` directive option pins their persisted owner identity.

`relation.Value[V]` declares a non-persisted computed slot. `DefineAggregates() ItsAggregateSet` supplies typed computations, and generation binds their concrete result getters/setters. The [aggregate guide](model-aggregates.md) describes counts, SQL-nullable extrema, exact-decimal sums/averages for integer and decimal fields, and float summaries. Numeric generated fields expose these operations; text, temporal and enum fields do not expose numeric aggregate methods.

Each model receives a field-set type, a `UserFields()`-style accessor, a table-named query entrypoint such as `QueryUsers()`, a model-specific query wrapper, and a `UserDraft`-style mutation draft. Query wrappers preserve concrete primary-key signatures through fluent derivation and share execution through `query.Query[M]`. The model declaration, its hydration codecs, field descriptors and bound relation/aggregate sets are immutable and built once on first use through a package-level `query.Memo`, so `QueryUsers()`, `UserFields()`, `UserRelations()` and `UserAggregates()` do not rebuild them per call; `DefineRelations` and `DefineAggregates` therefore run once per process and must not call their own generated accessor. `query.Memo` needs no package-variable initializer, so hooks and relation declarations that reach `QueryUsers()` cannot form a Go initialization cycle. Generated files remain in the model package, keeping handwritten methods separate and avoiding generated per-model subpackage cycles.

## Typed queries and drafts

This expression is exercised in the independent consumer:

```go
fields := models.UserFields()
q := models.QueryUsers().
    Where(fields.Age.Gte(18), fields.Status.Eq(models.StatusActive)).
    OrderBy(fields.Age.Desc())
err := q.Validate()
```

Fields retain their model owner and value type. A user query rejects an order predicate or order token; a user ID field rejects an order ID. Numeric fields expose range comparisons and do not expose text operations. Text fields add `Like` and literal `Contains`; nullable fields add `IsNull`/`IsNotNull`. `Eq` still takes the concrete non-null value type. Local and imported generated enum fields expose scalar membership operations and preserve their enum type. Recursive generation handles imported generated dependencies automatically when their packages are inside the selected scope.

`And`, `Or`, and `Not` compose expressions of the same model. Empty logical junctions and zero descriptors/predicates fail validation. `In` copies its input values and an empty membership set represents no matches. Query derivation copies owned slices, so changing one query does not change its source. `Validate` checks declaration boundaries and resource bounds; `Compile` and terminal operations also validate bound values through their typed codecs. The [query guide](model-queries.md) defines SQL execution and result semantics.

Normal application code uses generated descriptors. Public `query.New*Field` constructors require the concrete value codec; `query.Define`/`ForModel` declare ordered `query.Column` metadata and full-row hydration. Columns preserve nullability and database-default markers for mutation validation. Generated `query.NewModelField` declarations attach concrete getters and the same codecs for [cursor pagination](model-pagination.md) and relation keys. `query.For[M]` only describes a table. These are explicit boundaries for generated/extending code and do not certify that a physical database table/column exists.

A draft's zero value omits every field. `SetAge(0)` requests a zero write; `SetEmail("")` requests an empty string. `ClearNickname()` requests null; `UnsetNickname()` removes the field from the draft. Non-nullable fields have no `Clear` method. Getters return typed `value.Optional` states, with nested `Nullable` where appropriate. Methods return new drafts; they do not mutate the original. Required-field validation and execution are not implied by constructing a draft.

A [custom mutator](model-mutators.md#different-input-and-stored-types) may accept a concrete input type different from the stored field. Draft getters/setters and conflict-literal `Set` then use that input type, while comparisons, codecs, relation keys and stored changes retain the field type. Generated field notices document this distinction automatically. Drafts redact their values through `fmt.Formatter`; the persisted field name `Format` is reserved to avoid a generated method collision.

[Soft deletion](model-soft-deletes.md) is inferred from a persisted nullable `DeletedAt` instant. Generation adds typed deletion/restoration helpers and managed field notices through the same declaration pipeline. `soft_deletes=true` requires the convention; `soft_deletes=false` disables it.

## Enums and language tooling

Attach `//foundry:enum` to an exported string/integer type with typed constants. Its cases are the exported constants of that type declared as values. Aliases whose value is another case (`const DefaultStatus = StatusActive`, or `Status(StatusActive)`) and constants marked `//foundry:ignore` (on the constant, as its line comment, or on a const group's documentation) keep their Go meaning without becoming wire values. An unexported constant with its own value, such as a `statusCount` sentinel, fails generation with its position: export it to keep it as a case, or mark it `//foundry:ignore`. Directives may be written `//foundry:enum` or `// foundry:enum`. Two cases with the same serialized value fail with both names. The generator emits a source-ordered values list, parsing, membership validation, and text/JSON marshaling. Values slices are independent copies. Invalid casts remain possible in Go, so decoding, serialization, and explicit `Validate` enforce membership at runtime. Integer parsing checks its underlying width. Null input is rejected and failed decoding leaves the receiver unchanged. Enum strings must be valid UTF-8; pointer-sized `uintptr` is unsupported.

Generated enums expose a `StatusCodec()`-style typed codec and implement `database/sql/driver.Valuer` and `database/sql.Scanner` by delegating to it. The shared codec calls the generated `Validate` rule at both boundaries. String enums bind as strings; integer enums bind as signed `int64`, rejecting unsigned values outside the SQL integer range. Scanning accepts text/bytes and, for integer enums, `int64`; invalid values, overflow, floats, booleans and SQL null are rejected without modifying the receiver. `codec.Nullable(models.StatusCodec())` handles explicit null. Real PostgreSQL consumer tests verify valid and malformed stored enums through both direct codecs and generated full-model hydration.

An enum value's `EnumDescriptor()` returns a typed `enum.Descriptor[YourEnum]` independent of the receiver's value. Generated enum, DTO, message, validation-field, path, query, multipart and configuration-key descriptors are immutable and built once on first use through a package-level `sync.OnceValue`; generic DTO descriptors are built per call because each instantiation differs. Its cases preserve constant names, values and declaration order. `Definition()` produces a validated serialization boundary for future contract emitters, preserving large integer values as exact JSON rather than `float64`. Custom descriptors must preserve the scalar wire type and round-trip value. This descriptor does not expose a model as a public response DTO or generate an HTTP/TypeScript contract by itself.

The emitted code is available to ordinary Go editors and gopls. The [agent language tooling guide](agent-language-tooling.md) covers completion, hover, and definition over unsaved buffers, verified with real gopls against the independent consumer's generated types.

Generated scoped field sets also work inside [explicit correlations](model-correlations.md). Their signatures retain both outer and inner scope identities, concrete IDs and operator capabilities. Nullable join scopes require the generated nullable field accessor; correlation does not turn those fields into untyped expressions.

The same generator also emits [typed HTTP path bindings](http-path-generation.md)
from handwritten path structs; it shares declaration discovery, output ownership,
fresh-checkout support and stale checks with models and enums.

[Generated JSON DTO contracts](http-dtos.md) derive strict, typed transport schemas
from explicit public structs. They reuse the same generation pipeline and existing
enum descriptors, with independent response fields and omitted/null patch states.
Persistence models do not automatically become public DTOs. `//foundry:dto role=response`
declares a DTO that is never decoded from requests: it keeps its JSON descriptor
but omits the `ValidationFields` set and its type.

Milestone 19's explicit [`projection dto=true` opt-in](model-projections.md#public-report-dtos)
adds the same JSON contract and validation selectors to a declared report
projection. It shares ordinary DTO validation and the existing projection output
file. Fresh, repeated and check-only generation passed milestone 19 verification.

[Typed query generation](http-queries.md) uses `//foundry:query` structs for required, optional and repeated URL parameters through the same discovery, publication and stale-check workflow.

## Persisted binary fields

Generated `[]byte` and named byte-slice fields use the shared binary codec, typed nullable descriptors and owned mutation inputs. See [binary model values](binary-models.md) for supported operations, empty/NULL semantics and identity-key restrictions.
