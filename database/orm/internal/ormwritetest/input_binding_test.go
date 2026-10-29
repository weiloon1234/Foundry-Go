package ormwritetest

import "github.com/weiloon1234/Foundry-Go/database/orm"

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
	orm.Table[byteInputRow, int64]
	Body orm.InputColumn[byteInputRow, inputBytes, storedBytes]
}

func TestInputBuffersSurviveCallerAndMutatorChanges(t *testing.T) {
	fields := orm.Define("binary_inputs", func(b orm.Builder[byteInputRow], m *byteInputRow) byteInputTable {
		return byteInputTable{orm.TableOf(b, &m.ID), orm.InputBytesOf(b, &m.Body, func(v inputBytes) (storedBytes, error) { v[0]++; return storedBytes(v), nil })}
	})
	if err := orm.Check(fields); err != nil {
		t.Fatal(err)
	}
	var bindings [][]any
	db := testDB(t, testConnector{bindings: &bindings, columns: []string{"id", "body"}, values: [][]driver.Value{{int64(1), []byte{2, 2}}}})
	input := inputBytes{1, 2}
	a := orm.InputColumnSet(fields.Body, input)
	input[0] = 9
	for range 2 {
		_, err := orm.Insert(t.Context(), db, fields.Table, orm.KeySet(fields.Key(), 1), a)
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
	orm.Table[naturalInputKeyRow, string]
	Code orm.InputKey[naturalInputKeyRow, naturalKeyInput, string]
}

func TestExplicitInputKeyUsesSinglePrimaryDeclaration(t *testing.T) {
	fields := orm.Define("natural_input_keys", func(b orm.Builder[naturalInputKeyRow], m *naturalInputKeyRow) naturalInputKeys {
		table := orm.TableOf(b, &m.Code)
		return naturalInputKeys{table, orm.InputKeyOf(b, table.Key(), func(v naturalKeyInput) (string, error) { return v.Value, nil })}
	})
	if err := orm.Check(fields); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{columns: []string{"code"}, values: [][]driver.Value{{"MY"}}})
	got, err := orm.Insert(t.Context(), db, fields.Table, orm.InputKeySet(fields.Code, naturalKeyInput{Value: "MY"}))
	if err != nil || got.Code != "MY" {
		t.Fatal("explicit natural input key", err)
	}
	_, err = orm.RequireKey(t.Context(), db, fields.Table, "MY")
	if err != nil {
		t.Fatal(err)
	}
}
func TestInputKeyRejectsForeignAndRepeatedPrimaryBinding(t *testing.T) {
	first := orm.Define("first_key", func(b orm.Builder[naturalInputKeyRow], m *naturalInputKeyRow) orm.Table[naturalInputKeyRow, string] {
		return orm.TableOf(b, &m.Code)
	})
	foreign := orm.Define("foreign_key", func(b orm.Builder[naturalInputKeyRow], m *naturalInputKeyRow) orm.Table[naturalInputKeyRow, string] {
		table := orm.TableOf(b, &m.Code)
		orm.InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, first.Key(), nil)
		return table
	})
	duplicate := orm.Define("duplicate_key", func(b orm.Builder[naturalInputKeyRow], m *naturalInputKeyRow) orm.Table[naturalInputKeyRow, string] {
		table := orm.TableOf(b, &m.Code)
		orm.InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, table.Key(), nil)
		orm.InputKeyOf[naturalInputKeyRow, naturalKeyInput](b, table.Key(), nil)
		return table
	})
	if orm.Check(foreign) == nil || orm.Check(duplicate) == nil {
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
	fields := orm.Define("same_input_uuid", func(b orm.Builder[sameInputUUID], m *sameInputUUID) orm.Table[sameInputUUID, model.ID[sameInputUUID]] {
		table := orm.TableOf(b, &m.ID)
		orm.InputKeyOf[sameInputUUID, model.ID[sameInputUUID]](b, table.Key(), nil)
		return table
	})
	if err := orm.Check(fields); err != nil {
		t.Fatal(err)
	}
	db := testDB(t, testConnector{respondBound: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		return []string{"id"}, [][]driver.Value{{args[0].Value}}, nil
	}})
	got, err := orm.Insert(t.Context(), db, fields)
	if err != nil || got.ID.IsZero() {
		t.Fatal("same-type key lost UUID preparation", err)
	}
}
