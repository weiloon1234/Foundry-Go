package binarymodels

import (
	"encoding/hex"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Payload []byte
type Input []byte

//foundry:model table=binary_records primary=ID
type Record struct {
	ID int
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	// Foundry field behavior (generated): Record.Body retains stored binary_records.body. Custom setter: [Record.MutateBody] transforms assigned values during persistence through [RecordDraft.SetBody]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts binarymodels.Input and produces stored binarymodels.Payload; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	Body Payload
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	Raw []byte
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	// Foundry field behavior (generated): Record.Note retains stored binary_records.note. Custom setter: [Record.MutateNote] transforms assigned values during persistence through [RecordDraft.SetNote]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts binarymodels.Input and produces stored binarymodels.Payload; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Note value.Nullable[Payload]
	// Foundry field behavior (generated): Record.Encoded retains stored binary_records.encoded. Custom setter: [Record.MutateEncoded] transforms assigned values during persistence through [RecordDraft.SetEncoded]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts binarymodels.Input and produces stored string; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	Encoded string
}

func (Record) MutateBody(v Input) (Payload, error)   { return Payload(slices.Clone(v)), nil }
func (Record) MutateNote(v Input) (Payload, error)   { return Payload(slices.Clone(v)), nil }
func (Record) MutateEncoded(v Input) (string, error) { return hex.EncodeToString(v), nil }

//foundry:projection
type View struct {
	Body Payload
	Note value.Nullable[Payload]
}

func Draft(id int, input Input) RecordDraft {
	return RecordDraft{}.SetID(id).SetBody(input).SetRaw([]byte{}).ClearNote().SetEncoded(input)
}

func Matching(body Payload) query.Predicate[Record] { return RecordFields().Body.Eq(body) }
func NullableView() query.Expression[Record, value.Nullable[Payload]] {
	return RecordFields().Note.Value()
}
func Nulls() query.Predicate[Record]                      { return RecordFields().Note.IsNull() }
func ChangeBody(input Input) query.ConflictUpdate[Record] { return RecordFields().Body.Set(input) }
func BinaryChanges(before, after Record) (RecordChanges, error) {
	return CompareRecord(value.Set(before), value.Set(after), RecordDraft{}.SetBody(Input(after.Body)))
}
