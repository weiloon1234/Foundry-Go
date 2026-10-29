package orm

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestBinaryPredicatesOwnLazyBindings(t *testing.T) {
	type fields struct {
		Table[byteInputRow, int64]
		Body Column[byteInputRow, storedBytes]
	}
	f := Define("binary_records", func(b Builder[byteInputRow], m *byteInputRow) fields {
		return fields{TableOf(b, &m.ID), BytesOf(b, &m.Body)}
	})
	input := storedBytes{1, 2}
	predicates := []Predicate[byteInputRow]{ColumnEq(f.Body, input), ColumnNe(f.Body, input), ColumnIn(f.Body, input)}
	input[0] = 9
	for _, p := range predicates {
		s, err := Compile(f.Query().Where(p))
		if err != nil || !reflect.DeepEqual(s.Arguments(), []any{[]byte{1, 2}}) {
			t.Fatal("predicate borrowed binary input", s.Arguments(), err)
		}
	}
}

func TestBinaryAssignmentsAndMutatorsOwnReusableInputs(t *testing.T) {
	var f byteInputTable
	inspections := 0
	f = Define("binary_inputs", func(b Builder[byteInputRow], m *byteInputRow) byteInputTable {
		return byteInputTable{TableOf(b, &m.ID), InputBytesOf(b, &m.Body, func(v inputBytes) (storedBytes, error) { v[0]++; return storedBytes(v), nil })}
	}, WithHooks(func() Hooks[byteInputRow] {
		return Hooks[byteInputRow]{Creating: func(_ context.Context, _ *database.Tx, w *Write[byteInputRow]) error {
			first, ok := WriteGet(w, func() func(arg0 inputBytes) Assignment[byteInputRow] {
				receiver := f.Body
				return func(arg0 inputBytes) Assignment[byteInputRow] { return InputColumnSet(receiver, arg0) }
			}()).Get()
			if !ok || !reflect.DeepEqual(first, inputBytes{1, 2}) {
				t.Fatal("assignment borrowed input", first)
			}
			first[0] = 7
			second, ok := WriteGet(w, func() func(arg0 inputBytes) Assignment[byteInputRow] {
				receiver := f.Body
				return func(arg0 inputBytes) Assignment[byteInputRow] { return InputColumnSet(receiver, arg0) }
			}()).Get()
			if !ok || !reflect.DeepEqual(second, inputBytes{1, 2}) {
				t.Fatal("draft reader exposed input", second)
			}
			inspections++
			return nil
		}}
	}))
	var bindings [][]any
	db := hookDB(t, testConnector{bindings: &bindings, columns: []string{"id", "body"}, values: [][]driver.Value{{int64(1), []byte{2, 2}}}})
	input := inputBytes{1, 2}
	a := InputColumnSet(f.Body, input)
	input[0] = 9
	for range 2 {
		if _, err := Insert(t.Context(), db, f.Table, KeySet(f.Key(), 1), a); err != nil {
			t.Fatal(err)
		}
	}
	if inspections != 2 || !reflect.DeepEqual(bindings, [][]any{{int64(1), []byte{2, 2}}, {int64(1), []byte{2, 2}}}) {
		t.Fatal("reusable assignment changed", inspections, bindings)
	}
}
