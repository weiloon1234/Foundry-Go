package orm

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/model"
	"reflect"
	"testing"
)

type inputBytes []byte
type storedBytes []byte
type byteInputRow struct {
	ID   int64
	Body storedBytes
}
type byteInputTable struct {
	Table[byteInputRow, int64]
	Body InputColumn[byteInputRow, inputBytes, storedBytes]
}

func TestInputBuffersSurviveCallerAndMutatorChanges(t *testing.T) {
	fields := Define("binary_inputs", func(b Builder[byteInputRow], m *byteInputRow) byteInputTable {
		return byteInputTable{TableOf(b, &m.ID), InputBytesOf(b, &m.Body, func(v inputBytes) (storedBytes, error) { v[0]++; return storedBytes(v), nil })}
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, columns: []string{"id", "body"}, values: [][]driver.Value{{int64(1), []byte{2, 2}}}})
	input := inputBytes{1, 2}
	a := fields.Body.Set(input)
	input[0] = 9
	for range 2 {
		_, err := fields.Insert(t.Context(), db, fields.Key().Set(1), a)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(bindings, [][]any{{int64(1), []byte{2, 2}}, {int64(1), []byte{2, 2}}}) {
		t.Fatal("input snapshot changed across writes", bindings)
	}
}

type naturalKeyInput struct{ Value string }
type naturalInputKeyRow struct{ Code string }
type naturalInputKeys struct {
	Table[naturalInputKeyRow, string]
	Code InputKey[naturalInputKeyRow, naturalKeyInput, string]
}

func TestExplicitInputKeyUsesSinglePrimaryDeclaration(t *testing.T) {
	fields := Define("natural_input_keys", func(b Builder[naturalInputKeyRow], m *naturalInputKeyRow) naturalInputKeys {
		table := TableOf(b, &m.Code)
		return naturalInputKeys{table, InputKeyOf(b, table.Key(), func(v naturalKeyInput) (string, error) { return v.Value, nil })}
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{columns: []string{"code"}, values: [][]driver.Value{{"MY"}}})
	got, err := fields.Insert(t.Context(), db, fields.Code.Set(naturalKeyInput{Value: "MY"}))
	if err != nil || got.Code != "MY" {
		t.Fatal("explicit natural input key", err)
	}
	_, err = fields.Require(t.Context(), db, "MY")
	if err != nil {
		t.Fatal(err)
	}
}
func TestInputKeyRejectsForeignAndRepeatedPrimaryBinding(t *testing.T) {
	first := Define("first_key", func(b Builder[naturalInputKeyRow], m *naturalInputKeyRow) Table[naturalInputKeyRow, string] {
		return TableOf(b, &m.Code)
	})
	foreign := Define("foreign_key", func(b Builder[naturalInputKeyRow], m *naturalInputKeyRow) Table[naturalInputKeyRow, string] {
		table := TableOf(b, &m.Code)
		InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, first.Key(), nil)
		return table
	})
	duplicate := Define("duplicate_key", func(b Builder[naturalInputKeyRow], m *naturalInputKeyRow) Table[naturalInputKeyRow, string] {
		table := TableOf(b, &m.Code)
		InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, table.Key(), nil)
		InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, table.Key(), nil)
		return table
	})
	if Check(foreign) == nil || Check(duplicate) == nil {
		t.Fatal("foreign or repeated input key accepted")
	}
}

type sameInputUUID struct{ ID model.ID[sameInputUUID] }

func (sameInputUUID) MutateID(v model.ID[sameInputUUID]) (model.ID[sameInputUUID], error) {
	if v.IsZero() {
		return v, errors.New("UUID preparation skipped")
	}
	return v, nil
}
func TestSameTypePrimaryMutatorRetainsUUIDPreparation(t *testing.T) {
	fields := Define("same_input_uuid", func(b Builder[sameInputUUID], m *sameInputUUID) Table[sameInputUUID, model.ID[sameInputUUID]] {
		return TableOf(b, &m.ID)
	})
	if err := Check(fields); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{respondBound: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		return []string{"id"}, [][]driver.Value{{args[0].Value}}, nil
	}})
	got, err := fields.Insert(t.Context(), db)
	if err != nil || got.ID.IsZero() {
		t.Fatal("same-type key lost UUID preparation", err)
	}
}
