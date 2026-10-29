package binarymodels

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestBinaryDraftAndSnapshotsOwnBuffers(t *testing.T) {
	input := Input{1, 2}
	raw := []byte{3}
	stop := errors.New("draft inspected")
	called := false
	var table RecordTable
	table = declareRecords(orm.WithHooks(func() orm.Hooks[Record] {
		return orm.Hooks[Record]{Creating: func(_ context.Context, _ *database.Tx, draft *orm.Write[Record]) error {
			called = true
			body, _ := orm.WriteGet(draft, func() func(arg0 Input) orm.Assignment[Record] {
				receiver := table.Body
				return func(arg0 Input) orm.Assignment[Record] { return orm.InputColumnSet(receiver, arg0) }
			}()).Get()
			storedRaw, _ := orm.WriteGet(draft, func() func(arg0 []byte) orm.Assignment[Record] {
				receiver := table.RawBytes
				return func(arg0 []byte) orm.Assignment[Record] { return orm.ColumnSet(receiver, arg0) }
			}()).Get()
			note, _ := orm.WriteNullableGet(draft, func() func(arg0 Input) orm.Assignment[Record] {
				receiver := table.Note
				return func(arg0 Input) orm.Assignment[Record] { return orm.NullableInputColumnSet(receiver, arg0) }
			}()).Get()
			nullable, _ := note.Get()
			if body[0] != 1 || storedRaw[0] != 3 || nullable[0] != 1 {
				return errors.New("draft retained caller storage")
			}
			body[0], storedRaw[0], nullable[0] = 8, 8, 8
			body, _ = orm.WriteGet(draft, func() func(arg0 Input) orm.Assignment[Record] {
				receiver := table.Body
				return func(arg0 Input) orm.Assignment[Record] { return orm.InputColumnSet(receiver, arg0) }
			}()).Get()
			storedRaw, _ = orm.WriteGet(draft, func() func(arg0 []byte) orm.Assignment[Record] {
				receiver := table.RawBytes
				return func(arg0 []byte) orm.Assignment[Record] { return orm.ColumnSet(receiver, arg0) }
			}()).Get()
			note, _ = orm.WriteNullableGet(draft, func() func(arg0 Input) orm.Assignment[Record] {
				receiver := table.Note
				return func(arg0 Input) orm.Assignment[Record] { return orm.NullableInputColumnSet(receiver, arg0) }
			}()).Get()
			nullable, _ = note.Get()
			if body[0] != 1 || storedRaw[0] != 3 || nullable[0] != 1 {
				return errors.New("draft getter exposed owned storage")
			}
			return stop
		}}
	}))
	draft := []orm.Assignment[Record]{orm.KeySet(table.Key(), 1), orm.InputColumnSet(table.Body, input), orm.ColumnSet(table.RawBytes, raw), orm.NullableInputColumnSet(table.Note, input), orm.InputTextSet(table.Encoded, input)}
	input[0], raw[0] = 9, 9
	if _, err := orm.Insert(t.Context(), draftPool(t), table.Table, draft...); !errors.Is(err, stop) || !called {
		t.Fatal("draft inspection did not run", err)
	}
	before := Record{ID: 1, Body: Payload{1}, Raw: []byte{}, Note: value.Of(Payload{2})}
	after := Record{ID: 1, Body: Payload{3}, Raw: []byte{}, Note: value.Null[Payload]()}
	change, err := BinaryChanges(before, after)
	if err != nil {
		t.Fatal(err)
	}
	bodyChange, err := orm.ChangeOf(change, orm.InputColumnExpr(Records.Body))
	if err != nil {
		t.Fatal(err)
	}
	noteChange, err := orm.NullableChangeOf(change, orm.NullableInputColumnExpr(Records.Note))
	if err != nil || !bodyChange.Changed() || !noteChange.Changed() {
		t.Fatal("binary changes incorrect", err)
	}
	before.Body[0], after.Body[0] = 7, 7
	b, _ := change.Before().Get()
	a, _ := change.After().Get()
	if b.Body[0] != 1 || a.Body[0] != 3 {
		t.Fatal("model snapshot retained caller storage")
	}
	b.Body[0], a.Body[0] = 6, 6
	b, _ = change.Before().Get()
	a, _ = change.After().Get()
	if b.Body[0] != 1 || a.Body[0] != 3 {
		t.Fatal("model snapshot getter exposed owned storage")
	}
	field, _ := bodyChange.Before().Get()
	field[0] = 5
	field, _ = bodyChange.Before().Get()
	if !bytes.Equal(field, []byte{1}) {
		t.Fatal("field snapshot getter exposed owned storage")
	}
}
