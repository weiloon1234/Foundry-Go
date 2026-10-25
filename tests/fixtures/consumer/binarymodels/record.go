package binarymodels

import (
	"encoding/hex"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
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

// Draft captures input buffers; assignments preserve omitted, NULL and empty values.
func Draft(id int, input Input) []orm.Assignment[Record] {
	return []orm.Assignment[Record]{Records.Key().Set(id), Records.Body.Set(input), Records.RawBytes.Set([]byte{}), Records.Note.SetNull(), Records.Encoded.Set(input)}
}

func Matching(body Payload) orm.Predicate[Record] { return Records.Body.Eq(body) }
func NullableView() expr.Expression[Record, Payload, value.Nullable[Payload]] {
	return expr.Value(Records.Note)
}
func Nulls() orm.Predicate[Record]                      { return Records.Note.IsNull() }
func ChangeBody(input Input) orm.ConflictUpdate[Record] { return orm.ConflictSet(Records.Body, input) }
func BinaryChanges(before, after Record) (orm.Changes[Record], error) {
	return orm.Compare(Records.Table, value.Set(before), value.Set(after), Records.Body.Set(Input(after.Body)))
}
