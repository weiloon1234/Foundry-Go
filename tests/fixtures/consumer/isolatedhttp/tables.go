package isolatedhttp

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RecordTable declares the stored record fields. Example: RecordRows.Query().
type RecordTable struct {
	// Table owns the primary key. Example: RecordRows.Key().Eq(id).
	orm.Table[Record, model.ID[Record]]
	// Name binds the Name field. Example: RecordRows.Name.Set("fixture").
	Name orm.Text[Record, string]
	// Text binds the Text field. Example: RecordRows.Text.Set("text").
	Text orm.Text[Record, string]
}

// RecordRows preserves the persisted table and codecs. Example: RecordRows.Query().
var RecordRows = orm.Define("scope_records", func(b orm.Builder[Record], m *Record) RecordTable {
	return RecordTable{
		Table: orm.TableOf(b, &m.ID),
		Name:  orm.TextOf(b, &m.Name),
		Text:  orm.TextOf(b, &m.Text),
	}
})

// Tables lists declarations for startup validation. Example: orm.Check(Tables()...).
func Tables() []orm.CheckedTable { return []orm.CheckedTable{RecordRows} }
