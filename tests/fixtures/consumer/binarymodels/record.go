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

type Record struct {
	ID      int
	Body    Payload
	Raw     []byte
	Note    value.Nullable[Payload]
	Encoded string
}

func (Record) MutateBody(v Input) (Payload, error)   { return Payload(slices.Clone(v)), nil }
func (Record) MutateNote(v Input) (Payload, error)   { return Payload(slices.Clone(v)), nil }
func (Record) MutateEncoded(v Input) (string, error) { return hex.EncodeToString(v), nil }

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
