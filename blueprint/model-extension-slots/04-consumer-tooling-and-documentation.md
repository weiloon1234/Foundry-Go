# E04 — Consumer fixture, tooling and documentation

Prerequisites: E01–E03. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Independent consumer

The `articles` scenario is delivered to the [independent consumer](../../tests/fixtures/consumer/README.md).
It is a framework acceptance example, not a starter product, and uses only public
imports. It declares an `Article` with two translated fields, a single logo, a
gallery with a thumbnail variant, a typed metadata value and an ordinary author
relation. It assembles through `application.New` with native slot declarations and exercises list, show, create, update and
delete over real HTTP, PostgreSQL and a local disk:

- a public list with request-locale titles and logo and gallery file details from
  loaded slots (the local disk has no public URLs; link derivation parity is
  covered by E02);
- an edit screen loading explicitly declared slot relations;
- JSON and multipart create/update with slot-derived validation, `SyncIn`,
  metadata `SaveIn` and post-commit `ReplaceFile`/`AddFilesTo`;
- hard delete removing extension rows and files, and soft delete preserving them.

The existing [profiles consumer](../../tests/fixtures/consumer/profiles/profile.go)
uses the same native declaration owners. The guide preserves owner/storage
identity when migrating existing data to handwritten tables and handles.

## Tooling

- **Inspection.** An `inspection` section lists declared owners and slots (kind,
  stored name and policy summary) from the registered declarations. It opens no
  database and constructs no service, following the existing
  [inspection contract](../../docs/guides/developer-tooling-and-testing.md#doctor-and-declaration-inspection).
- **Undeclared stored names.** A read-only command in the existing
  `translations`/`metadata`/`attachments` command families lists stored field,
  key and collection names that no current slot declares, for example after a Go
  field rename that accidentally changes an explicit stored name. It prints opaque keys and names only
  and has no delete flag, like the orphan commands.
- **Scaffolds.** `foundry make model` writes handwritten ORM declarations and
  explicit native handles for `--translated`, `--attachment`, `--attachments` and
  `--metadata`. Attachment flags require `--disk` and policies accept no media
  until configured. Check-first publication and retained DTO generation stay in
  their existing owners; model policies are not generated.

## Documentation

- A consumer guide, `docs/guides/model-extension-slots.md`, covers declaration,
  stored-name and owner-rename rules, loading, pure reads, writes, HTTP input,
  validation, cleanup, transactions, limits and measured cost. Link it from the
  [model extensions](../../docs/guides/model-extensions.md),
  [attachments](../../docs/guides/attachments.md),
  [relations](../../docs/guides/model-relations.md) and
  [localization](../../docs/guides/localization.md) guides.
- [Model declarations](../../docs/guides/model-generation.md) explain handwritten
  fields and the retained DTO/transport generation boundary.
- The [agent language tooling](../../docs/guides/agent-language-tooling.md)
  scenarios gain slot completion and hover probes.
- The changelog records delivered behavior. The compatibility policy records the
  explicit owner identity and stored-name rules.

## Acceptance

The consumer compiles and passes against real PostgreSQL and HTTP. Inspection
output is a deterministic snapshot. The undeclared-name command is covered on
renamed-field data. Scaffold output compiles and generates. Public documentation
examples agree with the executable consumer.

Finish all implementation, documentation and tests, then execute the common
milestone gate.
