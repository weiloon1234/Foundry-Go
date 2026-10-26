# Enum and transport generation

Persistence models, table declarations and row projections are handwritten Go.
Use [model scaffolding](model-scaffolding.md), [typed reads](model-queries.md),
[writes](model-writes.md), [relations](model-relations.md) and
[projections](model-projections.md). Generation owns separate enum and transport
contracts; it does not emit tables, query namespaces, drafts or model accessors.
The [migration guide](orm-migration.md) retains historical before examples.

## Commands and ownership

[Client-contract publication](client-contracts.md#ownership-checking-and-recovery)
reuses this publisher and recovery command in a separate client output directory.
Go and client ownership manifests cannot share a directory.

Run commands with the native Go toolchain. From the framework repository:

```sh
go run ./cmd/foundry generate --recursive --dir tests/fixtures/consumer
go run ./cmd/foundry generate --recursive --check --dir tests/fixtures/consumer
```

`make generate` and `make generate-check` run these commands for every generation directory, adding `--field-docs` to clean obsolete managed field notices. An installed `foundry` command accepts the same arguments from a consumer workspace. Omit `--recursive` to target one existing Go package; include it to select the Go packages matched by `./...` below `--dir`. Nested modules, vendor, testdata, `node_modules`, hidden/underscore directories and go.mod `ignore` directories remain separate scopes. Module resolution is read-only and honors the consumer's module/workspace selection; a module with `vendor/modules.txt`, or an explicit `-mod` in `GOFLAGS`, keeps Go's own module mode instead of `-mod=readonly`.

Packages that use cgo or are excluded by this platform's build constraints are skipped when they contain no Foundry declarations. A Foundry declaration in a cgo file, or in a file excluded by build constraints, fails with that file's path instead of treating its owned output as obsolete: generated output has no build constraints, so declarations must live in unconstrained files of pure Go packages.

`foundry generate` compares the framework version it was built from with the version the consumer module selects. A known different release fails before analysis; run the module-pinned `go tool foundry` (see [team adoption](team-adoption.md)). Development builds and local `replace` directories cannot be compared and are accepted. `foundry doctor` reports the same comparison as its `tool-version` check.

Diagnostics name module-relative source paths such as `models/user.go:12:6`. A failed `--check` lists every stale target with its reason: `missing`, `obsolete`, `content differs`, `comments differ`, `formatting differs`, `managed field notes differ` or `ownership manifest differs`. Generated headers name their declaration's source file without a line number, so adding lines above a declaration does not rewrite other generated files.

Packages are processed in dependency order; independent packages are analyzed concurrently with a bounded worker count, and errors are reported in the same dependency order as a sequential run. Only dependencies outside the selection are compiled for export data; selected packages are type-checked from source once for declarations and once as the complete overlay, and packages without Foundry directives skip the declaration pass. Generated dependencies are shared as checked Go packages in memory, so a fresh checkout can contain handwritten methods that return generated types from other selected packages. Every selected package is validated before any output is published. Import paths beginning with `-` fail at their source location before dependency compilation, including when tolerant Go package metadata omits them; package operands are separated from Go command options. Import cycles, invalid dependents, stale files, and concurrent package/module-file changes fail generation. Dependencies outside the selected tree must already compile; select the appropriate consumer module root when those dependencies also need generation.

Generation discovers enum, DTO, validation, configuration, message, multipart,
union and HTTP path/query declarations, then checks the complete package with its
generated overlay before publication. Generic DTOs retain their concrete type
parameters. Persistence declarations are handwritten; checked `orm.Define`
instantiations identify model types after overlay checking. Those models cannot
become transport fields, embeddings or map keys. Use a separate DTO.

Generated-symbol collisions with handwritten declarations or other generated
symbols fail with source positions. Unrelated business code must also type-check;
a declaration-only success cannot publish an invalid complete package.

Commit the generated `*_foundry.gen.go` files and `.foundry-gen.json` alongside the handwritten source. The manifest records content hashes and owns deletion of obsolete files. Do not hand-edit generated files. Unknown, edited, orphaned, or non-regular output files are rejected rather than overwritten. Missing owned files can be regenerated. `--check` does not publish or rewrite project files, and unchanged generation leaves file modification times alone.

Publication rechecks source/target state, stages synced output and backups, records a recovery journal, replaces individual files by atomic rename, and writes ownership last. Ordinary failures and cancellation roll back applied changes. File permissions are preserved. If an editor changes a published file during failure handling, rollback retains the journal and refuses to overwrite that edit.

By default generation never edits handwritten files. `--field-docs` removes
obsolete managed getter/mutator notices left by the retired persistence generator.
It does not generate persistence methods or new field notices. Managed notice
cleanup shares publication, code-preservation checks and recovery with generated
outputs; ordinary handwritten comments remain owned by the author.

Field-notice publication retains an immutable new-source proof, so recovery can
survive another interruption after restoring an old handwritten file. Older
version 4 journals remain readable: recovery preserves validated new bytes before
restoration, or leaves an already-restored exact old target untouched when an old
writer consumed the new bytes. Changed targets and invalid proofs remain errors.


On the implemented Unix advisory-lock platforms, the next ordinary generation automatically recovers an interrupted publication. `foundry generate --recover --dir generation-root` performs recovery alone, using the scope recorded in the journal; omit `--recursive` for this command. Recovery keeps a completely published set or restores an incomplete set, including deleted and newly created outputs. It verifies hashes, file types, permissions and backups before restoration, and refuses to run while another publisher holds the process guard. `--check` never performs recovery or creates staging/guards.

Recursive publication uses one journal at the selected root and guards every participating package. Recovery makes one decision across that whole graph, including when one package had already finished publication before the interruption. A child cannot generate or recover independently while it belongs to a pending ancestor journal. Relative target paths are checked against recorded package directories, and filesystem operations use Go's `os.Root` confinement. Ordinary per-package manifests still own generated files, so later targeted generation remains possible after the graph publication finishes.

The advisory-lock adapter covers Linux, macOS, FreeBSD, NetBSD, OpenBSD and DragonFly; runtime verification covers Linux and native macOS. Other platforms use an exclusive directory guard and require manual inspection after a crash. Keep `.foundry-generate.guard` in place even when idle: deleting its inode while another process holds it would break locking. Ignore that guard and `.foundry-generate.lock/` in version control. Finished staging is moved to a unique `.foundry-generate.finished-*` directory before cleanup; a crash during cleanup may leave that completed staging for later removal. Do not delete retained journal/backups after a recovery error until their changes have been inspected. Individual file replacement and recovery do not make the whole output set one atomic filesystem transaction.

## Enums and language tooling

Attach `//foundry:enum` to an exported string/integer type with typed constants. Its cases are the exported constants of that type declared as values. Aliases whose value is another case (`const DefaultStatus = StatusActive`, or `Status(StatusActive)`) and constants marked `//foundry:ignore` (on the constant, as its line comment, or on a const group's documentation) keep their Go meaning without becoming wire values. An unexported constant with its own value, such as a `statusCount` sentinel, fails generation with its position: export it to keep it as a case, or mark it `//foundry:ignore`. Directives may be written `//foundry:enum` or `// foundry:enum`. Two cases with the same serialized value fail with both names. The generator emits a source-ordered values list, parsing, membership validation, and text/JSON marshaling. Values slices are independent copies. Invalid casts remain possible in Go, so decoding, serialization, and explicit `Validate` enforce membership at runtime. Integer parsing checks its underlying width. Null input is rejected and failed decoding leaves the receiver unchanged. Enum strings must be valid UTF-8; pointer-sized `uintptr` is unsupported.

Generated enums expose a `StatusCodec()`-style typed codec and implement `database/sql/driver.Valuer` and `database/sql.Scanner` by delegating to it. The shared codec calls the generated `Validate` rule at both boundaries. String enums bind as strings; integer enums bind as signed `int64`, rejecting unsigned values outside the SQL integer range. Scanning accepts text/bytes and, for integer enums, `int64`; invalid values, overflow, floats, booleans and SQL null are rejected without modifying the receiver. `codec.Nullable(models.StatusCodec())` handles explicit null. Real PostgreSQL consumer tests verify valid and malformed stored enums through both direct codecs and native ORM model hydration.

An enum value's `EnumDescriptor()` returns a typed `enum.Descriptor[YourEnum]` independent of the receiver's value. Generated enum, DTO, message, validation-field, path, query, multipart and configuration-key descriptors are immutable and built once on first use through a package-level `sync.OnceValue`; generic DTO descriptors are built per call because each instantiation differs. Its cases preserve constant names, values and declaration order. `Definition()` produces a validated serialization boundary for registered contract exporters, preserving large integer values as exact JSON rather than `float64`. Custom descriptors must preserve the scalar wire type and round-trip value. This descriptor does not expose a model as a public response DTO or generate an HTTP/TypeScript contract by itself.

Handwritten field help is retained on generated JSON property selectors and
transport validation fields. Leading and trailing comments remain editable on
the original field; regeneration updates their generated copies. These comments
do not implicitly export schema descriptions.

The emitted code is available to ordinary Go editors and gopls. The
[agent language tooling guide](agent-language-tooling.md) covers completion,
hover and definition over unsaved buffers.

## Transport declarations

[HTTP path bindings](http-path-generation.md), [query strings](http-queries.md),
[JSON DTO contracts](http-dtos.md), validation, configuration, messages,
multipart and unions share discovery, fresh-checkout support, publication and
stale-output checks. Enums reuse their generated descriptors. Explicit
`//foundry:dto role=response` declarations retain their JSON descriptor without
request-validation fields. Generic DTO descriptors are constructed per concrete
instantiation.

Persistence models are never implicitly public responses. A row projection may
hydrate a separate DTO whose transport declaration is generated independently;
its SQL mapping remains handwritten. [Typed JSON payloads](json-models.md) retain
generated property-path contracts without generating persistence columns.

## Persisted binary fields

Handwritten binary columns use the shared codec and typed declaration roles.
See [binary model values](binary-models.md) for empty/NULL semantics, operations
and identity-key restrictions.
