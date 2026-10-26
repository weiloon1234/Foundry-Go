# Generator changes

These instructions add to the [repository rules](../../AGENTS.md). Changes here
produce public consumer APIs even though the implementation package is private.
Read the closest discovery/emitter/tests and the corresponding runtime descriptor.

## Source and output ownership

- Do not generate persistence models, SQL query families or projections. Retired
  `model`/`projection` directives must fail with migration diagnostics before
  publication. Keep ORM scaffolds editable; see the [migration guide](../../docs/guides/orm-migration.md).
- Retain DTO, enum, config, message, multipart, union, path and HTTP query/form
  generation, contract export and TypeScript generation. HTTP `query` directives
  are not SQL queries. Keep shared codecs, JSON-path contract rules and source help
  reachable from retained declarations, without a hidden model generator.
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
- Remove obsolete managed model notes through source-edit publication, preserving
  handwritten comments and recovery for pre-upgrade journals. Follow the
  [fixture evidence rules](../../tests/fixtures/AGENTS.md) for frozen legacy outputs.
  Persistence uses handwritten ORM declarations; transport generation remains independent.

## Cross-layer acceptance

A new declaration shape is complete only when discovery, generation, runtime use
and consumer typing agree. Select the evidence appropriate to the changed shape:

- Valid output compiles in the independent consumer; invalid declarations and
  wrong owner/value uses fail for the intended reason.
- Fresh, repeated and current-output checks agree byte-for-byte. Edits remove only
  files whose ownership the existing publisher can establish.
- DTO/config changes reach their runtime codecs and manifest/client adapters;
  generic, nullable, union and map cases retain the same meaning.
- Model-generator retirement must leave retained manifest/OpenAPI/TypeScript and
  transport bytes identical. Ownership manifests may change through publication.
  Retire model-only tests only after their native replacement assertions exist;
  keep mixed contract, filesystem and recovery coverage.
- Public signature or documentation changes have real gopls completion/hover/definition
  coverage through the [existing agent tests](../agent), using unsaved overlays.
- Publication/recovery changes retain real filesystem/crash/failure evidence.

Reuse existing compiler/editor harnesses and external-input tracking. Do not create
one cold Go cache per case or discard ownership journals merely to make tests pass.
