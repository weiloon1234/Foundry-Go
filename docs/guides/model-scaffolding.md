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

The scaffold supplies a per-model `WidgetTables()` helper. Compose it with other
models in an application-owned `Tables()` function, shown below. Check that list
with `orm.Check(Tables()...)` and pass it to `application.WithTables(Tables()...)`
where the application uses table registration. No runtime package discovery runs.

## Add fields with lifecycle roles

The scaffold has no timestamp, soft-delete, mutator or ordinary relation flags.
The following compiled example extends its initial struct and table declaration:

<!-- compiled-source: tests/fixtures/goway/guideadapters/scaffold_test.go -->
```go
type NameInput struct{ Text string }

type Widget struct {
	ID   model.ID[Widget] `foundry:"column=id"`
	Name string
	// CreatedAt preserves supplied input; omission uses the owning clock.
	CreatedAt temporal.DateTime
	// UpdatedAt is managed by the write pipeline.
	UpdatedAt temporal.DateTime
	DeletedAt value.Nullable[temporal.DateTime]
	Children  relation.Many[Child]
}

type WidgetTable struct {
	orm.Table[Widget, model.ID[Widget]]
	Name      orm.InputText[Widget, NameInput, string]
	CreatedAt orm.CreatedAt[Widget, temporal.DateTime]
	UpdatedAt orm.UpdatedAt[Widget, temporal.DateTime]
	DeletedAt orm.SoftDelete[Widget, temporal.DateTime]
	Soft      orm.SoftDeletable[Widget]
	Children  orm.Many[Widget, Child]
}

var Widgets = orm.Define("widgets", func(b orm.Builder[Widget], m *Widget) WidgetTable {
	deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
	return WidgetTable{
		Table: orm.TableOf(b, &m.ID),
		Name: orm.InputTextOf(b, &m.Name, func(input NameInput) (string, error) {
			return strings.TrimSpace(input.Text), nil
		}),
		CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt),
		UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt),
		DeletedAt: deleted,
		Soft:      orm.SoftDeletes(deleted),
		Children:  orm.ManyOf(b, &m.Children),
	}
})
```

Creation time preserves supplied input and uses the owning clock when omitted.
Update time is managed by the write pipeline. Document both on fields and handles.
`SoftDeletes` derives the capability needed for physical deletion. `InputTextOf`
keeps the stored string separate from `NameInput`; predicates accept the stored
string, while `orm.InputTextSet` accepts the input type. A nil transform selects a named model
mutator. Other `Input*` families preserve their corresponding storage types.

## Add relations after table declarations

Bind relation slots inside each `orm.Define`, then declare computations after both
tables. This avoids initialization cycles:

<!-- compiled-source: tests/fixtures/goway/guideadapters/scaffold_test.go -->
```go
type Child struct {
	ID       model.ID[Child]
	WidgetID model.ID[Widget]
}

type ChildTable struct {
	orm.Table[Child, model.ID[Child]]
	WidgetID orm.Column[Child, model.ID[Widget]]
}

var Children = orm.Define("children", func(b orm.Builder[Child], m *Child) ChildTable {
	return ChildTable{orm.TableOf(b, &m.ID), orm.ColumnOf(b, &m.WidgetID)}
})
var WidgetChildren = orm.HasManyOf(Widgets.Children, orm.KeyExpr(Widgets.Key()), orm.ColumnExpr(Children.WidgetID))

func WidgetTables() []orm.CheckedTable { return []orm.CheckedTable{Widgets} }
func ChildTables() []orm.CheckedTable  { return []orm.CheckedTable{Children} }

func Tables() []orm.CheckedTable {
	return append(WidgetTables(), ChildTables()...)
}
```

Load with `Widgets.Query().With(WidgetChildren)`. See the
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
ORM field comments are ordinary handwritten documentation. Plain model scaffolds
run no generation.

The [scaffold tests](../../internal/generate/scaffold_model_test.go) compile the
output and exercise `orm.Check`, typed roles and relation declarations. The
[plain model](../../internal/generate/testdata/scaffold/model.golden) and
[extension model](../../internal/generate/testdata/scaffold/extensions.golden)
goldens show complete initial files.

For the old-to-new API mapping, see [ORM migration](orm-migration.md).

The ORM examples are checked against the [compiled guide examples](../../tests/fixtures/goway/guideadapters) and the linked consumer sources.
