package binarymodels

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RecordTable separates mutation input from stored binary values.
// Example: Records.Body.Set(Input{1,2}); Records.Body.Eq(Payload{1,2}).
type RecordTable struct {
	orm.Table[Record, int]
	Body     orm.InputColumn[Record, Input, Payload]
	RawBytes orm.Column[Record, []byte]
	Note     orm.NullableInputColumn[Record, Input, Payload]
	Encoded  orm.InputText[Record, Input, string]
}

// Records owns binary buffers at the assignment and hydration boundaries.
// Example: Records.Insert(ctx,db,Draft(1,Input{1})...).
var Records = declareRecords()

func declareRecords(options ...orm.Option) RecordTable {
	return orm.Define("binary_records", func(b orm.Builder[Record], m *Record) RecordTable {
		return RecordTable{orm.TableOf(b, &m.ID), orm.InputBytesOf[Record, Input](b, &m.Body, nil), orm.BytesOf(b, &m.Raw), orm.NullableInputBytesOf[Record, Input](b, &m.Note, nil), orm.InputTextOf[Record, Input](b, &m.Encoded, nil)}
	}, options...)
}

// ViewRow declares an explicit DTO; a partial selection never hydrates Record.
// Example: rows.Map(Views.Body,expr.Value(Records.Body)).
type ViewRow struct {
	rows.Row[View]
	Body rows.Field[View, Payload]
	Note rows.Field[View, value.Nullable[Payload]]
}

// Views preserves empty byte slices and SQL NULL in DTO reads.
// Example: rows.Select(rows.From(Records.Query()),Views.Row,...).
var Views = rows.Define(func(b rows.Builder[View], v *View) ViewRow {
	return ViewRow{b.Row(), rows.BytesOf(b, &v.Body), rows.NullableBytesOf(b, &v.Note)}
})
