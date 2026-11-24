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
