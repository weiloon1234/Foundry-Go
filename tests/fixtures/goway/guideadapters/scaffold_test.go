package guideadapters_test

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// guide:scaffold-roles
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

// guide:end

// guide:scaffold-relations
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

// guide:end
