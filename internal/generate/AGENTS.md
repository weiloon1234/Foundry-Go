# Generator changes

These instructions add to the [repository rules](../../AGENTS.md). Changes here
produce public consumer APIs even though the implementation package is private.
Read the closest discovery/emitter/tests and the corresponding runtime descriptor.

## Source and output ownership

- Generate tables and identity blocks from handwritten `//foundry:model` structs
  and `foundry` tags. Emit one `<file>_foundry.gen.go` per source file through the
  existing publisher. Do not generate SQL query families, drafts or projections.
  The `projection` directive remains retired. See the [declaration guide](../../docs/guides/model-declarations.md).
- Retain DTO, enum, config, message, multipart, union, path and HTTP query/form
  generation, contract export and TypeScript generation. HTTP `query` directives
  are not SQL queries. Keep shared codecs, JSON-path contract rules and source help
  reachable from model and transport declarations.
- Use parsed/type-checked Go declarations and existing metadata owners. Reuse the
  shared runtime JSON/config/type rules instead of approximating them with a second
  reflection or string-parsing implementation.
- Preserve concrete owners, generic type arguments, named scalar widths, optional/
  nullable states and typed identifiers in retained contract signatures. Recognize
  native persistence declarations without weakening DTO field/embedding/map-key rejection.
  Unsupported or ambiguous declarations must fail with actionable source diagnostics.
- Generated documentation and symbols are part of IDE experience. Keep deterministic
  ordering, imports, names and comments, including repeat generation after edits.
- Change handwritten declarations and emitters, not `*_foundry.gen.*` files. Finish
  the source/test batch before running compilation-backed generation and verification.
  Use [Makefile](../../Makefile) generation ordering for retained contracts and
  ownership cleanup. Keep directive-free owned packages reachable until cleanup
  is published; remove only obsolete owned outputs from mixed packages.
- Preserve ownership manifests, checksums, locking, guarded atomic publication and
  recovery behavior. Never overwrite unknown/user-owned files or infer permission
  from a filename alone. Keep declaration/source and generated output lifetimes distinct.
- Generation leaves handwritten model comments unchanged. Reject unsupported
  journals without changing their staging. Follow the
  [fixture evidence rules](../../tests/fixtures/AGENTS.md) for frozen legacy outputs.
  Transport generation remains independent of model table generation.
- Reuse `internal/modelmeta` for tags and fingerprints. Keep the compile-time
  structural assertion and declaration-time ordered field/tag fingerprint.
  Directive and field-documentation comment changes require `generate --check`. Mirror static ORM rules
  and retain executable tests of advertised timestamp corrections.
- Preserve constructor coverage in the model generator tests. Low-level functions
  are generated-code implementation APIs and test tools; direct use outside the
  ORM tree is limited to tests, generic models and runtime-computed table names.
- Keep `internal/modelconvert` and its ORM snapshot bridge isolated. They exist
  only for `foundry models convert` and are removed in the release after consumers
  convert; no runtime feature may depend on them.

## Cross-layer acceptance

A new declaration shape is complete only when discovery, generation, runtime use
and consumer typing agree. Select the evidence appropriate to the changed shape:

- Valid output compiles in the independent consumer; invalid declarations and
  wrong owner/value uses fail for the intended reason.
- Fresh, repeated and current-output checks agree byte-for-byte. Edits remove only
  files whose ownership the existing publisher can establish.
- DTO/config changes reach their runtime codecs and manifest/client adapters;
  generic, nullable, union and map cases retain the same meaning.
- Model declaration changes must preserve unrelated manifest/OpenAPI/TypeScript
  and transport bytes. Keep mixed contract, filesystem and recovery coverage.
  Retired query/draft APIs remain retired; model directives generate typed ORM
  declarations, not those former APIs.
- Public signature or documentation changes have real gopls completion/hover/definition
  coverage through the [existing agent tests](../agent), using unsaved overlays.
- Publication/recovery changes retain real filesystem/crash/failure evidence.

Reuse existing compiler/editor harnesses and external-input tracking. Do not create
one cold Go cache per case or discard ownership journals merely to make tests pass.
