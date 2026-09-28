# Generator changes

These instructions add to the [repository rules](../../AGENTS.md). Changes here
produce public consumer APIs even though the implementation package is private.
Read the closest discovery/emitter/tests and the corresponding runtime descriptor.

## Source and output ownership

- Use parsed/type-checked Go declarations and existing metadata owners. Reuse the
  shared runtime JSON/config/type rules instead of approximating them with a second
  reflection or string-parsing implementation.
- Preserve concrete owners, generic type arguments, named scalar widths, optional/
  nullable states, field capabilities and separate mutation inputs in signatures.
  Unsupported or ambiguous declarations must fail with actionable source diagnostics.
- Generated documentation and symbols are part of IDE experience. Keep deterministic
  ordering, imports, names and comments, including repeat generation after edits.
- Change handwritten declarations and emitters, not `*_foundry.gen.*` files. Finish
  the source/test batch before running compilation-backed generation and verification.
  Framework-owned models are generated before dependent consumers; use the existing
  generation ordering in [Makefile](../../Makefile).
- Preserve ownership manifests, checksums, locking, guarded atomic publication and
  recovery behavior. Never overwrite unknown/user-owned files or infer permission
  from a filename alone. Keep declaration/source and generated output lifetimes distinct.

## Cross-layer acceptance

A new declaration shape is complete only when discovery, generation, runtime use
and consumer typing agree. Select the evidence appropriate to the changed shape:

- Valid output compiles in the independent consumer; invalid declarations and
  wrong owner/value uses fail for the intended reason.
- Fresh, repeated and current-output checks agree byte-for-byte. Edits remove only
  files whose ownership the existing publisher can establish.
- DTO/config changes reach their runtime codecs and manifest/client adapters;
  generic, nullable, union and map cases retain the same meaning.
- Public signature or documentation changes have real gopls completion/hover/definition
  coverage through the [existing agent tests](../agent), using unsaved overlays.
- Publication/recovery changes retain real filesystem/crash/failure evidence.

Reuse existing compiler/editor harnesses and external-input tracking. Do not create
one cold Go cache per case or discard ownership journals merely to make tests pass.
