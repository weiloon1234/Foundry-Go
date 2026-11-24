# Start a model declaration

<a id="agent-entry"></a>
## Agent entry

- [Declare a model](#recipe-scaffold-model)
- [Register tables](model-declarations.md#recipe-register-tables)
- [Add fields](#recipe-add-fields)
- [Add relations](#recipe-add-relations)
- [Add extension slots](#recipe-scaffold-slots)

<a id="recipe-scaffold-model"></a>
## Declare a model

Run the scaffold in an existing model package. It writes a struct with
`//foundry:model` and a per-model registration helper. Like the DTO scaffold, it
prints the next generation command; it does not publish generated output.

```sh
GOWORK=off go tool foundry make model Widget --table widgets --dir ./models
GOWORK=off go tool foundry generate --dir ./models
GOWORK=off go tool foundry generate --check --dir ./models
GOWORK=off go tool foundry models check --dir ./models
```

Generation writes the table type, variable, field bindings and identity block to
`<file>_foundry.gen.go`. Include the generated table in the application's explicit
registry. It does not create SQL tables or a package-wide `Tables()` helper.

The scaffolded source is compiled with its generated table:

<!-- compiled-source: tests/fixtures/goway/modelguide/widget_model.go -->
```go
package modelguide

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// Widget is a persisted model. Add domain fields, then run foundry generate.
// Example: Widget{}.
//
//foundry:model table=widgets key=ID var=Widgets
type Widget struct {
	// ID is the concrete owner key. Example: record.ID.
	ID model.ID[Widget]
}

// WidgetTables supplies this declaration to the package's Tables helper.
// Example: orm.Check(WidgetTables()...).
func WidgetTables() []orm.CheckedTable { return []orm.CheckedTable{Widgets} }

// Declare relations after the tables they reference to avoid initialization cycles.
```

<a id="recipe-add-fields"></a>
## Add fields with lifecycle roles

Add the stored field to the struct and regenerate. Types infer ordinary handles.
Use `input=NameInput` with a value-receiver `MutateName` method or a named
`mutate` function. Use `role=created-at` and `role=updated-at` for a managed pair,
and `role=soft-delete` for a nullable deletion instant. The latter generates
`Widgets.Soft`. See [roles and diagnostics](model-declarations.md#fields-and-roles).

<a id="recipe-add-relations"></a>
## Add relations after generation

Add `relation.One[Child]` or `relation.Many[Child]` fields to the model and
regenerate their typed slots. Declare the relationship computations after both
tables, using the generated key and foreign-key handles. Keep table initialization
separate from relation setup to avoid cycles. See [relations](model-relations.md).

<a id="recipe-scaffold-slots"></a>
## Add extension slots

Add `translations.Text`, `attachments.One[Model]`, `attachments.Many[Model]` or
`metadata.Value[T]` fields and regenerate. Their types identify slots; no `slot`
tag is needed. Keep stored names, policies and owner identities in handwritten
extension declarations. Reuse the generated identity variable in the model's
package and the generated slot handles in `extensions.BindSlot`.

Register extension declarations through `application.Builder.Models`, configure
their disks and specify attachment media policies. Metadata DTOs still need their
own generated codec. See the [complete slot fixture](../../tests/fixtures/consumer/articles/article.go)
and [extension slot guide](model-extension-slots.md).
