# E01 — Slot declarations and generation

The filename retains the original milestone identity. Model-slot generation was
retired in Stage 8; native declarations now own the same storage and loading
contracts. The [master](../00-master-architecture-and-parity.md#model-extension-slot-delivery)
records historical acceptance; the [current guide](../../docs/guides/model-extensions.md)
and [articles consumer](../../tests/fixtures/consumer/articles/tables.go) define the
supported API.

## Slot types and declarations

| Model field | Native policy handle | Stored value |
| --- | --- | --- |
| `translations.Text` | `translations.TextOf` | immutable locale values |
| `attachments.One[M]` | `attachments.OneOf` | optional ready file |
| `attachments.Many[M]` | `attachments.ManyOf` | ordered ready files |
| `metadata.Value[V]` | `metadata.ValueOf` | optional typed value |

Bind each field with `orm.SlotOf` in `orm.Define`. `extensions.BindSlot` connects
its validated offset and primary key to the policy handle. Stored names are explicit
handle arguments, not generator defaults. Preserve them across Go field renames.
Slots are not SQL columns or assignment targets and refuse implicit response
serialization. DTOs explicitly map loaded values. Not loaded, loaded empty and
present remain distinct; reading never issues I/O. Loaded collections return
owned slices; arbitrary nested values retain ordinary Go value-copy semantics.

Metadata contracts use explicit/inferred typed descriptors with stored versions.
Translation options retain locale requirements, bounds and exact synchronization.
Attachment policies retain disks, media, image plans, cardinality and publication
rules. Wrong-owner bindings fail validation; startup rejects missing policy or
manager configuration. Existing manager limits own per-owner/key/collection bounds.

## Owner identity and assembly

`extensions.DefineORMOwner` takes a validated `orm.IdentityOf`, an explicit owner
name and `OwnerOptions`. Pin `StorageModel` before renaming a table. Owner name and
storage model form persisted scope; changing them can hide stored data without
deleting it. `PreviousModels` and explicit rescope commands remain recovery tools.

`extensions/slots` sits above the extension managers and holds immutable native
declarations and borrowed runtime managers. Application registration merges owners,
handles and cleanup observers before pools freeze their observer registrations.
Disabled required features fail startup before resource acquisition. No additional
registry, storage table or implicit service lookup is introduced.

Hard-delete cleanup runs through the native observer owner. Soft deletion preserves
slot data. Translation/metadata writes join explicit transactions; attachments keep
their recoverable publication workflow. Batch eager loading and existing-parent
loading reuse the [shared relation boundary](02-slot-loading-and-reads.md).

## Retained generation and acceptance

DTO, enum, validation, config and transport generation remain. Persistence models
and slot policies are handwritten; they cannot silently become transport DTOs.
The Makefile remains the only generation entry point. Native compiler negatives,
startup validation, PostgreSQL owner/cleanup tests, bounds, snapshots and editor
completion replace the retired model-generation cases. Frozen observations retain
historical evidence without executable generated declarations.
