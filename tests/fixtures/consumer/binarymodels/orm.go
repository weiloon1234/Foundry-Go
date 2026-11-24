package binarymodels

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/value"
)

// RecordTable separates mutation input from stored binary values.
// Example: orm.Set(Records.Body, Input{1,2}); orm.Eq(Records.Body, Payload{1,2}).

// Records owns binary buffers at the assignment and hydration boundaries.
// Example: orm.Insert(ctx, db, Records.Table,Draft(1,Input{1})...).

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
