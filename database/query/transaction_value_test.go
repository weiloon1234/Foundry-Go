package query

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type transactionValueInner struct{}

func TestTransactionValueCompositionGuardsOrdinaryExecution(t *testing.T) {
	type owner = TransactionAlias[transactionTestAlias, transactionTestRecord]
	outer := AsTransaction[transactionTestAlias](TransactionOf(transactionTestModel()), "outer_records")
	inner := AsTransaction[transactionValueInner](TransactionOf(transactionTestModel()), "inner_records")
	left := NewOrderedField[owner, int64]("outer_records", "id", codec.Signed[int64]())
	right := NewOrderedField[TransactionAlias[transactionValueInner, transactionTestRecord], int64]("inner_records", "id", codec.Signed[int64]())
	locked := SelectTransactionValue(inner, right.Value()).ForUpdate()
	ordinary := ForModel(Define("outer_records", "id", []Column{{Name: "id"}}, func(row database.Row) (owner, error) {
		var ignored int64
		err := row.Scan(&ignored)
		return owner{}, err
	}))
	member := TransactionInQuery(Add(left, left.Param(1)), locked)
	exists := TransactionExistsQuery(outer, locked)
	for _, p := range []Predicate[owner]{member, exists} {
		for _, kind := range []readKind{readModels, readCount, readExists} {
			statement, err := ordinary.Where(p).compile(kind)
			if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") || statement.SQL() != "" {
				t.Fatal("ordinary query accepted transaction predicate", kind, statement, err)
			}
		}
	}
	scalar := TransactionScalarQuery(outer, locked)
	if _, err := SelectValue(ordinary, scalar).Compile(); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "transaction-scoped") {
		t.Fatal("ordinary scalar accepted nested lock", err)
	}
	if _, err := SelectTransactionValue(outer, scalar).Compile(); err != nil {
		t.Fatal("transaction scalar lost valid permission", err)
	}
}

func TestTransactionComputedMembershipQualificationAndBindings(t *testing.T) {
	type owner = TransactionAlias[transactionTestAlias, transactionTestRecord]
	outer := AsTransaction[transactionTestAlias](TransactionOf(transactionTestModel()), "outer_records")
	field := NewOrderedField[owner, int64]("outer_records", "id", codec.Signed[int64]())
	baseField := NewOrderedField[transactionTestRecord, int64]("records", "id", codec.Signed[int64]())
	inner := TransactionValueOf(SelectValue(transactionTestModel(), baseField.Value()).Where(baseField.Gt(9)))
	predicate := TransactionInQuery(Add(field, field.Param(7)), inner)
	q := SelectTransactionRecord(outer, outer.Scope()).Where(predicate)
	statement, err := q.Compile()
	if err != nil || !reflect.DeepEqual(statement.Arguments(), []any{int64(7), int64(9)}) {
		t.Fatal("computed IN binding order", statement, err)
	}
	changed := requalify(predicate.expression, "renamed")
	for _, test := range []struct {
		input expression
		want  string
	}{{predicate.expression, "outer_records"}, {changed, "renamed"}} {
		var fields []string
		walk := selectWalk{localSelect: true, field: func(f fieldRef) { fields = append(fields, f.table) }}
		walk.expression(test.input, 0)
		if walk.err != nil || !reflect.DeepEqual(fields, []string{test.want}) {
			t.Fatal("membership qualification crossed SELECT phases", fields, walk.err)
		}
	}
	if changed.(subqueryPredicate).query.node.source.table != "records" {
		t.Fatal("membership renamed independent inner SELECT")
	}
	after, err := q.Compile()
	if err != nil || after.SQL() != statement.SQL() || !reflect.DeepEqual(after.Arguments(), statement.Arguments()) {
		t.Fatal("membership requalification mutated source", after, err)
	}
	var missing RowValue[owner, int64]
	if _, err := q.Where(TransactionInQuery(missing, inner)).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing membership operand became EXISTS", err)
	}
}
