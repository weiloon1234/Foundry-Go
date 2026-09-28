# 03 — Generation and language tooling

## Purpose and prerequisites

Prerequisite: [02](02-foundation-and-application-lifecycle.md). Its optional TOML adapter is not a generator dependency. Make ordinary Go declarations the SSOT for discoverable, compiler-checked framework APIs.

Rust references: `foundry-macros`, `foundry-build`, `src/app_enum`, `src/contract`, `tools/foundry-agent`, `tests/derive_ui.rs`, `tests/ui`, `docs/guides/ai-agent-code-intelligence.md`.

## Ownership and generation contract

`cmd/foundry` hosts `generate`, `generate --check`, and `agent` commands. A private generator pipeline owns parsing, type checking, normalized metadata, diagnostics, and writing. Model, enum, validation, and contract emitters share that pipeline; they must not build separate schema parsers.

- Model structs remain handwritten, normally grouped in a consumer's domain model package. Go structs are not regenerated from a parallel schema DSL.
- Use a `//foundry:model table=users` declaration directive and a `foundry` struct-tag namespace for persistence metadata. Table/column strings exist once at the declaration boundary and are validated during generation.
- Preserve named Go field types, nullability, and type parameters through the metadata representation. Do not erase a named enum or model ID to its underlying scalar type.
- Emit `<model>_foundry.gen.go` beside its owning model in the same package, avoiding generated per-model subpackage import cycles. Handwritten business methods and lifecycle implementations live in separate ordinary files.
- Hook and relation references are Go symbols/type-checked descriptors, not dynamically resolved string function names.
- Generated files include a standard generated-code header, source locations, and Go documentation. Stable ordering and formatting must not depend on timestamps, absolute paths, or map iteration.
- Milestone 07 extends documentation generation with [automatic getter/mutator notices on model fields](07-model-lifecycle-events-and-audit.md#automatic-field-documentation-for-getters-and-setters). Method discovery owns those notices; consumers do not maintain extra configuration. Managed source comments must share the existing publication/recovery guarantees while handwritten model behavior stays separate from generated implementation.

Delivered source declaration shape, using the foundational model ID:

```go
//foundry:model table=users
type User struct {
    ID    model.ID[User]
    Email string `foundry:"column=email"`
}
```

## Type-safety proof before ORM expansion

Generate prototype model fields, predicates, mutations and query types without a live database. Prove that Go rejects a wrong model owner, a wrong field value, and a wrong model ID. Compare completion/hover output against those generated signatures. Keep these as compile-pass/compile-fail fixtures when runtime execution arrives.

Use current Go features only after confirming compiler and gopls support. Concrete generic methods are available in Go 1.27; generic interface methods are not. Do not design an interface that depends on unsupported language syntax. [Go generic methods](https://go.dev/blog/generic-methods)

## Fresh-checkout and regeneration behavior

Support models whose handwritten methods reference generated symbols that are absent or stale. Use a declaration-only in-memory analysis phase to establish source types, generate an overlay, then type-check the complete package against that overlay. Do not require a fake generated file to be committed or suppress model declaration errors because unrelated generated methods are missing.

Generate into a temporary staging area, validate all outputs, and replace owned files atomically only after success. Delete only obsolete files owned by the generator's manifest. `--check` compares in memory and does not rewrite tracked files. A failed generation leaves the previous output intact.

## Agent and editor workflow

Provide `foundry agent complete`, `hover`, and `definition` with workspace, existing context file, and symbol/position inputs. Completion probes use unsaved LSP overlays, never source-file edits. Responses include structured JSON and readable text. Use the consumer's module graph and pinned Foundry version; do not return a static catalog pretending to be semantic completion.

Pin a compatible gopls as a development tool after approval. Generator output remains usable in ordinary Go editors without the Foundry agent command. [gopls](https://go.dev/gopls/)

The approved [gopls v0.23.0](https://go.dev/gopls/release/v0.23.0) supports Go 1.27 language features and is installed into the VM's host-mounted ignored `bin/`. [tools/gopls.version](../tools/gopls.version) owns the development-tool pin, consumed by explicit `make gopls-install`; it is not a framework runtime dependency. Real completion, hover, and definition tests pass against the independent consumer.

## Delivered generation slice and remaining work

The [generation guide](../docs/guides/model-generation.md) owns current CLI usage and supported declarations. `foundry generate --dir package-directory` analyzes one existing Go package; `--recursive` selects its Go package tree and processes dependencies before dependents. Completed generated packages are shared in memory, so fresh checkouts do not need existing export data for other selected packages. Every full package overlay is checked before publication. `--check` detects stale output without publication. Typed field operators, model-owned predicates/orders, drafts, aliases, UUID/self-reference fields, natural scalar keys, and enum text/JSON validation have independent consumer coverage.

Publication validates manifest ownership and content hashes, rechecks inputs, stages synced outputs/backups and a journal, atomically renames individual files, publishes the manifest last, and rolls back ordinary failures/cancellation. A process guard excludes concurrent publication/recovery on supported Unix platforms. Forced-termination tests cover retaining complete output, restoring incomplete output, and refusing changed targets. `generate --recover` and automatic recovery before ordinary generation are implemented; the guide records platform and cleanup limits. The entire multi-file set is not one filesystem-atomic rename.

Recursive runs keep one journal for the selected graph and acquire every participant's guard. All package outputs are recovered together; child commands cannot bypass a pending ancestor publication. Recorded paths and package directories are validated, and `os.Root` confines target operations. Ordinary manifests remain per package. Source/package membership and module/workspace inputs are checked again before publication.

Generated enum `Value`/`Scan` methods implement standard SQL boundary contracts with membership, null, and integer-range checks. Typed `enum.Descriptor[E]` values provide exact normalized JSON definitions for future contract emitters. Imported enum fields are recognized through their typed descriptor method and keep scalar-only operators. Dependency-aware declaration analysis supports generated aliases and ignored fields during fresh checkout bootstrap; complete-overlay checking still validates all handwritten declarations.

The [agent client](../docs/guides/agent-language-tooling.md) implements completion, hover, and definition in the consumer workspace. Protocol fixture tests cover unsaved buffers, UTF-8/UTF-16 conversion, read-only server requests, bounded framing, cancellation, and owned-process shutdown. The real gopls gate verifies generated draft methods, the absence of invalid nullable operations, model-owned ID hover signatures, and definition lookup into generated consumer source.

Run the real language-tooling gate with `FOUNDRY_TEST_GOPLS="$PWD/bin/gopls" make agent-smoke`. It requires an existing executable and verifies consumer source is unchanged. Ordinary protocol fixture responses alone do not satisfy this gate.

## Implementation and acceptance

1. Parse directives/tags and report source-positioned declaration errors.
2. Implement normalized metadata and deterministic model/enum prototype emission.
3. Implement overlay bootstrap, write ownership, and `--check`.
4. Add LSP session lifecycle and real-consumer completion smoke coverage.
5. Add generated enum parsing, validation, database-codec and contract descriptors; complete their runtime integrations in later milestones.

Test renamed/deleted fields, empty projects, duplicate tables/columns, unsupported types, aliases, generic IDs, self-references, package cycles, missing generated files, stale output, interrupted generation, and repeat generation with no diff. Apply the [common gate](README.md#common-completion-gate).
