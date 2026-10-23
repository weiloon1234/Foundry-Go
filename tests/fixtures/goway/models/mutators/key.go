package mutators

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// KeyRecord exercises a distinct primary-key input.
type KeyRecord struct {
	ID    model.ID[KeyRecord]
	Label string `foundry:"default=database"`
}
type FreshID struct{ Value model.ID[KeyRecord] }

func (KeyRecord) MutateID(input FreshID) (model.ID[KeyRecord], error) { return input.Value, nil }

type KeyRecordTable struct {
	orm.Table[KeyRecord, model.ID[KeyRecord]]
	ID    orm.InputKey[KeyRecord, FreshID, model.ID[KeyRecord]]
	Label orm.Text[KeyRecord, string]
}

var KeyRecords = orm.Define("input_key_records", func(b orm.Builder[KeyRecord], m *KeyRecord) KeyRecordTable {
	table := orm.TableOf(b, &m.ID)
	return KeyRecordTable{table, orm.InputKeyOf[KeyRecord, FreshID](b, table.Key(), nil), orm.TextOf(b, &m.Label)}
})
