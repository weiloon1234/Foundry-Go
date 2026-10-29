package rows_test

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestScopeBridgeRejectsZeroCapabilityWithValidMetadata(t *testing.T) {
	m := declareMembers()
	source := rows.As[left](m.Query(), "m")
	metadata := expr.FoundryScopeData(rows.Scope(source), sqlowner.Expressions())
	var zero sqlowner.ExpressionSeal
	rejected := expr.FoundryScope[rows.Alias[left, member], member](metadata, zero)
	actual := expr.FoundryScopeData(rejected, sqlowner.Expressions())
	if !errors.Is(actual.Err, fault.Invalid) || actual.Owner != nil || actual.Table != "" {
		t.Fatal("zero seal adopted scope", actual)
	}
}

func TestModelBridgesRejectZeroCapabilityWithValidMetadata(t *testing.T) {
	o := declareOrders()
	amount := expr.Value(orm.NumberExpr(o.MemberID))
	predicate := expr.FoundryPredicate(amount.Eq(1), sqlowner.Expressions())
	orderBy := expr.FoundryOrder(amount.Asc(), sqlowner.Expressions())
	var zero sqlowner.QuerySeal
	for _, q := range []orm.Query[order]{orm.FoundryWhere(o.Query(), predicate, zero), orm.FoundryOrderBy(o.Query(), orderBy, zero)} {
		state := &driverState{}
		got, err := orm.All(t.Context(), openTest(t, state), q)
		if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
			t.Fatal("zero capability applied valid metadata", got, err, state.sql)
		}
	}
}

func TestNullableScopeBridgesRejectZeroCapability(t *testing.T) {
	m := declareMembers()
	a, b := rows.As[left](m.Query(), "a"), rows.As[right](m.Query(), "b")
	j := rows.LeftJoin(a, b, rows.On(expr.At(rows.Scope(a), orm.KeyExpr(m.ID)), expr.At(rows.Scope(b), orm.KeyExpr(m.ID))))
	scope := rows.NullableRightScope(j, rows.Scope(b))
	metadata := expr.FoundryNullableScopeData(scope, sqlowner.Expressions())
	var zero sqlowner.ExpressionSeal
	rejected := expr.FoundryNullableScope[rows.Left[rows.Alias[left, member], rows.Alias[right, member]], member](metadata, zero)
	actual := expr.FoundryNullableScopeData(rejected, sqlowner.Expressions())
	if !errors.Is(actual.Err, fault.Invalid) || actual.Owner != nil {
		t.Fatal("zero capability adopted metadata", actual)
	}
	actual = expr.FoundryNullableScopeData(scope, zero)
	if !errors.Is(actual.Err, fault.Invalid) || actual.Owner != nil {
		t.Fatal("zero capability exposed metadata", actual)
	}
}

func TestSubqueryBridgesRejectZeroCapabilities(t *testing.T) {
	m := declareMembers()
	v := expr.Row(expr.Value(orm.KeyExpr(m.ID))).Value()
	row := expr.Value(orm.KeyExpr(m.ID))
	core := expr.FoundrySelected(v, sqlowner.Expressions())
	column := expr.FoundrySelectedColumn(v, sqlowner.Expressions())
	var zero sqlowner.ExpressionSeal
	if expr.FoundryRowValue(row, zero).Node != nil || expr.FoundrySelectedColumn(v, zero).Bind != nil {
		t.Fatal("zero capability exposed expression data")
	}
	selected := expr.FoundrySelectedValue[member](core, column, zero)
	restored := expr.FoundryRowExpression[member](core, column, zero)
	if !errors.Is(expr.FoundrySelected(selected, sqlowner.Expressions()).Err, fault.Invalid) || !errors.Is(expr.FoundryRowValue(restored, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("zero capability adopted a value")
	}
	predicate := expr.FoundryPredicateValue[member](expr.FoundryPredicate(row.Eq(1), sqlowner.Expressions()), zero)
	if !errors.Is(expr.FoundryPredicate(predicate, sqlowner.Expressions()).Err, fault.Invalid) {
		t.Fatal("zero capability adopted predicate")
	}
}
