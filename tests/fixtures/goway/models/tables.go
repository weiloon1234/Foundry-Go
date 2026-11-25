package models

import "github.com/weiloon1234/Foundry-Go/database/orm"

// doc-example:begin application-table-list
// Tables returns an owned list of declarations for startup validation.
// Example: application.WithTables(models.Tables()...).
func Tables() []orm.CheckedTable {
	return []orm.CheckedTable{Users.Table, Orders.Table, WriteRecords.Table}
}

// doc-example:end application-table-list
