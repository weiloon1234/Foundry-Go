package rows

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/countries"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
)

type namedBool bool

func TestBooleanAndFrameworkEnumConstructors(t *testing.T) {
	checkCell(t, BoolOf[cell[bool], bool], false, false)
	checkCell(t, BoolOf[cell[namedBool], namedBool], true, namedBool(true))
	checkCell(t, EnumOf[cell[countries.Status], countries.Status], string(countries.EnabledStatus), countries.EnabledStatus)
	if _, err := hydrateCell(t, EnumOf[cell[countries.Status], countries.Status], "not-a-status"); err == nil {
		t.Fatal("invalid framework enum accepted")
	}
	if _, err := hydrateCell(t, EnumOf[cell[countries.Status], countries.Status], nil); err == nil {
		t.Fatal("NULL enum accepted")
	}
}

func TestConstantCaseAndScopedRowParameterParity(t *testing.T) {
	d := declareAggregateRecords()
	source := As[left](d.Query(), "a")
	condition := expr.At(Scope(source), d.Exact).Eq(1)
	scoped := expr.At(Scope(source), d.Exact)
	before := expr.When(condition, expr.Row(scoped).Value().Param(int64(2))).Else(expr.Row(scoped).Value().Param(int64(0)))
	after := expr.When(condition, expr.Constant[Alias[left, aggregateRecord]](codec.Signed[int64](), 2)).Else(expr.Row(scoped).Param(0))
	a := expressionSQL(t, expressionQuery(source, before.Value(), codec.Signed[int64]()))
	b := expressionSQL(t, expressionQuery(source, after.Value(), codec.Signed[int64]()))
	if a.SQLText != b.SQLText || !reflect.DeepEqual(a.Bindings, b.Bindings) {
		t.Fatal("CASE SQL/bindings changed")
	}
	// Boolean constants need no unrelated Boolean column.
	flag := expr.When(condition, expr.Constant[Alias[left, aggregateRecord]](codec.Bool[bool](), true)).Else(expr.Constant[Alias[left, aggregateRecord]](codec.Bool[bool](), false))
	statement := expressionSQL(t, expressionQuery(source, flag.Value(), codec.Bool[bool]()))
	if !reflect.DeepEqual(statement.Bindings, []any{int64(1), true, false}) {
		t.Fatalf("Boolean CASE bindings: %#v", statement.Bindings)
	}
}
