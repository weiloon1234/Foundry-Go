# Scaffold an ORM model

Create an existing Go package, then scaffold its first model:

```sh
foundry make model Widget --table widgets --dir ./models
```

The command creates `widget_model.go` with a `Widget` struct, a `WidgetTable`
namespace, the `Widgets` declaration and `WidgetTables()`. The primary key remains
`model.ID[Widget]`, with an explicit `foundry:"column=id"` tag. Add fields and bind
them inside `orm.Define`; model edits need no generation. The command preserves
existing files and rejects declaration collisions before publication.

The flags are unchanged. There are no timestamp, soft-delete, mutator or ordinary
relation flags: add those declarations by hand. Only metadata DTOs and other
contract declarations still need `foundry generate`.

Scaffold names are allocated together, including metadata codec and validation
helpers. Collisions receive deterministic numeric suffixes; ordinary names stay
unchanged. For example, `--table widget_table` uses `WidgetTable1` for the table
variable and preserves the `WidgetTable` type. `--metadata Table` uses
`ArticleTable1` for an Article model's metadata DTO. Persisted table and slot names
do not change. If a valid SQL name yields an empty or digit-leading namespace,
the scaffold prefixes `Query`: `_` becomes `Query`, and `_9` becomes `Query9`.
The same rule applies to schema-qualified tables. SQL-name validation remains
unchanged; bare digit-leading and non-ASCII SQL identifiers are rejected.

## Register and check declarations

Compose the per-model helpers in one application-owned function. The scaffold
does not overwrite this function when you add another model:

```go
func Tables() []orm.CheckedTable {
    return append(WidgetTables(), OrderTables()...)
}
```

Check the declarations with `orm.Check(models.Tables()...)`. Where the application
assembly uses `WithTables`, pass this same explicit list to its table-registration
option. Add every model to the list; the scaffold performs no package-wide runtime
discovery. `WithTables` is application integration, not a method on the model table.

## Add fields with lifecycle roles

For managed timestamps, add `CreatedAt` and `UpdatedAt` fields of type
`temporal.DateTime`. Add the matching handles to `WidgetTable`:

```go
CreatedAt orm.CreatedAt[Widget, temporal.DateTime]
UpdatedAt orm.UpdatedAt[Widget, temporal.DateTime]
```

Bind them in the declaration:

```go
CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt),
UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt),
```

Creation time preserves supplied input and uses the owning clock when omitted.
Update time is managed by the write pipeline. Document those behaviors beside
the model fields and handles; do not use ordinary `TimeOf` for these roles.

For soft deletion, add `DeletedAt value.Nullable[temporal.DateTime]`, a
`DeletedAt orm.SoftDelete[Widget, temporal.DateTime]` handle and a
`Soft orm.SoftDeletable[Widget]` capability. Bind the field once and derive the
capability from that handle:

```go
deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
return WidgetTable{
    Table: orm.TableOf(b, &m.ID),
    DeletedAt: deleted,
    Soft: orm.SoftDeletes(deleted),
}
```

Keep the other declared fields in the returned table. The `Soft` capability is
required for physical deletion of a soft-deletable model.

For a mutated field, keep the stored model type and select an `Input*` handle for
its write input. For example, a stored `Name string` can accept a distinct
`NameInput`:

```go
type NameInput struct { Text string }
// In WidgetTable:
// Name orm.InputText[Widget, NameInput, string]

// In the WidgetTable literal inside Define:
Name: orm.InputTextOf(b, &m.Name, func(input NameInput) (string, error) {
    return strings.TrimSpace(input.Text), nil
}),
```

`Widgets.Name.Set` now accepts `NameInput`; predicates retain the stored `string`
type. Use the corresponding numeric, nullable, JSON or other `Input*` family for
other stored types. A nil transform selects the model's named mutator.

## Add relations after table declarations

Add a `Children relation.Many[Child]` field and an
`orm.Many[Widget, Child]` handle, bound with `orm.ManyOf(b, &m.Children)`. Declare
the computation after both tables, keeping it outside `orm.Define`:

```go
var WidgetChildren = orm.HasManyOf(
    Widgets.Children, Widgets.Key(), Children.WidgetID,
)
```

Load it with `Widgets.Query().With(WidgetChildren)`. This separates field offsets
from cross-table references and avoids initialization cycles. See the
[ORM API guide](../../database/orm/README.md) for other relation kinds.

## Add extension slots

```sh
foundry make model Article --table articles --translated Title \
  --attachment Logo --attachments Gallery --metadata SEO --disk public --dir ./models
```

The same handwritten file contains the slot fields, `orm.Slot` bindings (for example,
`Articles.TitleSlot`),
`ArticleExtensionOwner`, the typed `ArticleExtensions` handles, their `From` and
`All` methods, and `ArticleExtensionDeclaration()`. The owner and stored slot
names remain explicit. The `Slot` suffix keeps fields such as `Key`, `Table` and
`Query` from hiding table operations. Bind managers once with `ArticleExtensions.From(runtime)`;
load its relations with `Articles.Query().With(bound.All()...)`.

Register `ArticleExtensionDeclaration()` through `application.Builder.Models`.
List accepted attachment media types, or an image plan, and configure the disk
before assembly. An empty attachment policy still fails validation. Keep the
owner identity and stored slot names stable when renaming Go fields or tables.

`ArticleSEO` remains a generated-codec DTO. The scaffold generates its codec
before returning; run `foundry generate` after changing its JSON fields.
`--field-docs` still applies to the generation run by model-with-slots scaffolds;
ORM field comments are ordinary handwritten documentation. Plain model scaffolds
continue to reject this flag because they run no generation.

The [scaffold tests](../../internal/generate/scaffold_model_test.go) compile the
output and exercise `orm.Check`, typed roles and relation declarations. The
[plain model](../../internal/generate/testdata/scaffold/model.golden) and
[extension model](../../internal/generate/testdata/scaffold/extensions.golden)
goldens show complete initial files.
