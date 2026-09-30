# E04 — Consumer fixture, tooling and documentation

Prerequisites: E01–E03. Status belongs to the
[master](../00-master-architecture-and-parity.md#model-extension-slot-delivery).

## Independent consumer

Add an `articles` scenario to the [independent consumer](../../tests/fixtures/consumer/README.md).
It is a framework acceptance example, not a starter product, and uses only public
imports. It declares an `Article` with two translated fields, a single logo, a
gallery with a thumbnail variant, a typed metadata value and an ordinary author
relation. It assembles through `application.New` with
`Models(models.FoundryExtensions()...)` and exercises list, show, create, update and
delete over real HTTP, PostgreSQL and a local disk:

- a public list with request-locale titles and logo and gallery file details from
  loaded slots (the local disk has no public URLs; link derivation parity is
  covered by E02);
- an edit screen loading every slot through `All()`;
- JSON and multipart create/update with slot-derived validation, `SyncIn`,
  metadata `SaveIn` and post-commit `ReplaceFile`/`AddFiles`;
- hard delete removing extension rows and files, and soft delete preserving them.

The existing [profiles consumer](../../tests/fixtures/consumer/profiles/profile.go)
stays unchanged as the explicit-declaration example. The guide shows how to move
such a model to generated declarations using E01's scope-equality guarantee.

## Tooling

- **Inspection.** An `inspection` section lists declared owners and slots (kind,
  stored name and policy summary) from the registered declarations. It opens no
  database and constructs no service, following the existing
  [inspection contract](../../docs/guides/developer-tooling-and-testing.md#doctor-and-declaration-inspection).
- **Undeclared stored names.** A read-only command in the existing
  `translations`/`metadata`/`attachments` command families lists stored field,
  key and collection names that no current slot declares, for example after a Go
  field rename without `foundry:"name=..."`. It prints opaque keys and names only
  and has no delete flag, like the orphan commands.
- **Scaffolds.** `foundry make model` slot flags were not delivered in this
  series. Scaffolds keep their check-first publication; declaring slot fields
  and `DefineExtensions` by hand stays short, and an attachment policy always
  needs an explicit disk, which a scaffold could not choose.

## Documentation

- A consumer guide, `docs/guides/model-extension-slots.md`, covers declaration,
  stored-name and owner-rename rules, loading, pure reads, writes, HTTP input,
  validation, cleanup, transactions, limits and measured cost. Link it from the
  [model extensions](../../docs/guides/model-extensions.md),
  [attachments](../../docs/guides/attachments.md),
  [relations](../../docs/guides/model-relations.md) and
  [localization](../../docs/guides/localization.md) guides.
- [Model generation](../../docs/guides/model-generation.md) documents slot fields
  among supported field kinds.
- The [agent language tooling](../../docs/guides/agent-language-tooling.md)
  scenarios gain slot completion and hover probes.
- The changelog records delivered behavior. The compatibility policy records the
  generated owner identity and stored-name rules.

## Acceptance

The consumer compiles and passes against real PostgreSQL and HTTP. Inspection
output is a deterministic snapshot. The undeclared-name command is covered on
renamed-field data. Scaffold output compiles and generates. Public documentation
examples agree with the executable consumer.

Finish all implementation, documentation and tests, then execute the common
milestone gate.
