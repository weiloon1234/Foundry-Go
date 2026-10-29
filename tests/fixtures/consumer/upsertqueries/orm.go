package upsertqueries

import "github.com/weiloon1234/Foundry-Go/database/orm"

// ExcludedTable keeps database defaults and the distinct stored primary key.
// Example: orm.TextSet(ExcludedRecords.Name, "before").
type ExcludedTable struct {
	orm.Table[ExcludedRecord, ExcludedKey]
	Name orm.Text[ExcludedRecord, string]
}

// ExcludedRecords exercises quoting of the physical table named excluded.
// Example: orm.Upsert(ctx,tx,ExcludedRecords.Query(),policy,assignments...).
var ExcludedRecords = orm.Define("excluded", func(b orm.Builder[ExcludedRecord], m *ExcludedRecord) ExcludedTable {
	return ExcludedTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name)}
})
