package orm

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type interfaceComparisonRow struct {
	ID    int64
	Value any
}

type interfaceComparisonTable struct {
	Table[interfaceComparisonRow, int64]
	Value Column[interfaceComparisonRow, any]
}

func interfaceComparisons(encode func(any) (driver.Value, error)) interfaceComparisonTable {
	c := codec.New(encode, func(v any) (any, error) { return v, nil }).WithParameterType(codec.TypeText)
	return Define("interface_comparisons", func(b Builder[interfaceComparisonRow], row *interfaceComparisonRow) interfaceComparisonTable {
		return interfaceComparisonTable{Table: TableOf(b, &row.ID), Value: ColumnOf(b, &row.Value, Codec(c))}
	})
}

func TestCompileInterfaceComparisonNilValuesReachCodec(t *testing.T) {
	var inputs []any
	table := interfaceComparisons(func(v any) (driver.Value, error) {
		inputs = append(inputs, v)
		if v == nil {
			return "encoded nil", nil
		}
		return v.(string), nil
	})
	for _, tc := range []struct {
		name              string
		predicate         Predicate[interfaceComparisonRow]
		inputs, arguments []any
	}{
		{"equality", table.Value.Eq(nil), []any{nil}, []any{"encoded nil"}},
		{"membership", table.Value.In("first", nil, "last"), []any{"first", nil, "last"}, []any{`{"first","encoded nil","last"}`}},
		{"inequality", table.Value.Ne(nil), []any{nil}, []any{"encoded nil"}},
		{"not membership", table.Value.NotIn(nil, "last"), []any{nil, "last"}, []any{`{"encoded nil","last"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs = nil
			statement, err := Compile(table.Query().Where(tc.predicate))
			if err != nil || statement.SQL() == "" || !reflect.DeepEqual(statement.Arguments(), tc.arguments) || !reflect.DeepEqual(inputs, tc.inputs) {
				t.Fatalf("nil interface did not reach its declared codec: %v", err)
			}
		})
	}
}

func TestCompileInterfaceNullComparisonKeepsValidation(t *testing.T) {
	table := interfaceComparisons(func(v any) (driver.Value, error) { return v, nil })
	for _, predicate := range []Predicate[interfaceComparisonRow]{table.Value.Eq(nil), table.Value.In("first", nil, "last")} {
		statement, err := Compile(table.Query().Where(predicate))
		if !errors.Is(err, fault.Invalid) || errors.Is(err, fault.Panicked) || err.Error() != "invalid: NULL comparison requires IsNull or IsNotNull" || statement.SQL() != "" || len(statement.Arguments()) != 0 {
			t.Fatal("codec-owned NULL rejection changed classification, diagnostic or partial bindings")
		}
	}
}

func TestCompileCachedComparisonTypeMismatchKeepsContainedDiagnostic(t *testing.T) {
	table := records()
	for _, membership := range []bool{false, true} {
		for _, wrong := range []any{nil, int64(7)} {
			t.Run(fmt.Sprintf("membership=%v/nil=%v", membership, wrong == nil), func(t *testing.T) {
				predicate := table.Name.Eq("private literal")
				if membership {
					predicate = table.Name.In("first", "private literal")
				}
				// Simulate a stale/mismatched retained value, then exercise the
				// public compiler rather than invoking the adapter directly.
				node := querycore.PredicateNode(predicate.expression).(querycore.Comparison)
				values := querycore.ComparisonValues(node.Values)
				values[len(values)-1] = wrong
				statement, err := Compile(table.Query().Where(predicate))
				var detail *fault.Error
				if !errors.Is(err, fault.Panicked) || errors.Is(err, fault.Invalid) || !errors.As(err, &detail) || detail.Code() != fault.Panicked || err.Error() != "panicked: query comparison binding panicked" || statement.SQL() != "" || len(statement.Arguments()) != 0 {
					t.Fatal("adapter mismatch escaped containment or changed its typed diagnostic")
				}
			})
		}
	}
}
