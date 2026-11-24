package officequeries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// RegionTable binds Region storage and typed relation slots.

// OfficeTable binds Office storage and typed relation slots.

// EmployeeTable binds Employee storage and typed relation slots.

// NoteTable binds Note storage and typed relation slots.

// LabelTable binds Label storage and typed relation slots.

// LabelingTable binds Labeling storage and typed relation slots.

// DeskTable binds Desk storage and typed relation slots.

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable {
	return []orm.CheckedTable{Regions.Table, Offices.Table, Employees.Table, Notes.Table, Labels.Table, Labelings.Table, Desks.Table}
}

func foundryDeskScopes(t DeskTable) []orm.GlobalScope[Desk] {
	return []orm.GlobalScope[Desk]{OpenOfficeScope(t)}
}
