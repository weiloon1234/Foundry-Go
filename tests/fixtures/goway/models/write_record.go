package models

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RecordKey int64

// WriteRecord exercises database defaults through its handwritten table.
type WriteRecord struct {
	Code    RecordKey `foundry:"default=database"`
	Name    string
	Enabled bool `foundry:"default=database"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal `foundry:"default=database"`
}

type WriteRecordTable struct {
	orm.Table[WriteRecord, RecordKey]
	Code    orm.NumberLookupKey[WriteRecord, RecordKey]
	Name    orm.Text[WriteRecord, string]
	Enabled orm.Column[WriteRecord, bool]
	Note    orm.NullableText[WriteRecord, string]
	Amount  orm.Number[WriteRecord, decimal.Decimal]
}

var WriteRecords = orm.Define("write_records", func(b orm.Builder[WriteRecord], m *WriteRecord) WriteRecordTable {
	table := orm.TableOf(b, &m.Code)
	return WriteRecordTable{table, orm.NumberKey(table.Key()), orm.TextOf(b, &m.Name), orm.ColumnOf(b, &m.Enabled), orm.NullableTextOf(b, &m.Note), orm.NumberOf(b, &m.Amount)}
})
