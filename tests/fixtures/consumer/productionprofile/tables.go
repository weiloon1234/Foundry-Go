package productionprofile

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// RecordTable binds Record storage and typed relation slots.
type RecordTable struct {
	orm.Table[Record, model.ID[Record]]
	ID      orm.Key[Record, model.ID[Record]]
	Name    orm.Text[Record, string]
	Active  orm.Column[Record, bool]
	Score   orm.Number[Record, int64]
	Payload orm.Column[Record, []byte]
	Note    orm.NullableText[Record, string]
}

var Records = orm.Define("profile_records", func(b orm.Builder[Record], m *Record) RecordTable {
	table := orm.TableOf(b, &m.ID)
	return RecordTable{Table: table, ID: table.Key(), Name: orm.TextOf(b, &m.Name), Active: orm.ColumnOf(b, &m.Active), Score: orm.NumberOf(b, &m.Score), Payload: orm.BytesOf(b, &m.Payload), Note: orm.NullableTextOf(b, &m.Note)}
})

// Tables exposes thin declarations for application validation.
func Tables() []orm.CheckedTable { return []orm.CheckedTable{Records.Table} }
