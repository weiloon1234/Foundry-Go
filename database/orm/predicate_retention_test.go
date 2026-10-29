package orm_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Use concrete private fields: fmt deliberately ignores their formatting methods.
func assertRetentionRedacted[T any](t *testing.T, name string, input T, secret string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		assertPredicateRedacted(t, input, secret)
		assertPredicateRedacted(t, &input, secret)
		assertPredicateRedacted(t, struct{ private T }{input}, secret)
		assertPredicateRedacted(t, struct{ nested struct{ private T } }{struct{ private T }{input}}, secret)
		assertPredicateRedacted(t, struct{ nested any }{struct{ private T }{input}}, secret)
	})
}

type retentionLeft struct{}
type retentionRight struct{}

func TestPublicPredicateRetentionRedaction(t *testing.T) {
	const secret = "retained-literal-SECRET-945817"
	table := orm.Define("predicate_owners", func(b orm.Builder[predicateOwner], m *predicateOwner) orm.Table[predicateOwner, string] {
		return orm.TableOf(b, &m.ID)
	})
	p := orm.KeyEq(table.Key(), secret)
	q := table.Query().Where(p)
	value := expr.Value(orm.KeyExpr(table.Key()))
	param := expr.Param(orm.KeyExpr(table.Key()), secret)
	projected := expr.ModelPredicate(p)
	source := rows.From(q)
	scalar := rows.SelectScalar(source, param.Value())
	record := rows.ModelRecord(q)
	report := rows.SelectValue(q, orm.KeyExpr(table.Key()))
	window := expr.Window[predicateOwner]{}.PartitionBy(expr.Group(param)).OrderBy(param.Asc())
	agg := expr.Count[predicateOwner]().Filter(projected)

	direct, err := rows.CompileRead(rows.Read(report))
	if err != nil {
		t.Fatal(err)
	}
	adapted, err := rows.CompileRead(rows.Read(rows.SelectValue(table.Query(), orm.KeyExpr(table.Key())).Where(projected)))
	if err != nil {
		t.Fatal(err)
	}
	if direct.SQL() != adapted.SQL() || !reflect.DeepEqual(direct.Arguments(), adapted.Arguments()) || !reflect.DeepEqual(adapted.Arguments(), []any{secret}) {
		t.Fatal("predicate adapter changed SQL or binding")
	}
	assertRetentionRedacted(t, "orm/key-lookup", orm.Keyed(q, table.Key()), secret)
	assertRetentionRedacted(t, "orm/global-scope", orm.Scope(table, "private_scope", func(context.Context) (orm.Predicate[predicateOwner], error) { return p, nil }), secret)
	assertRetentionRedacted(t, "orm/query", q, secret)
	assertRetentionRedacted(t, "orm/locked-query", orm.Lock(q), secret)
	assertRetentionRedacted(t, "orm/bridge", orm.FoundryPredicate(p, sqlowner.Metadata()), secret)
	assertRetentionRedacted(t, "expr/model-predicate", projected, secret)
	assertRetentionRedacted(t, "expr/column-predicate", value.Eq(secret), secret)
	assertRetentionRedacted(t, "expr/row-param", param, secret)
	assertRetentionRedacted(t, "expr/selected", param.Value(), secret)
	assertRetentionRedacted(t, "expr/order", param.Asc(), secret)
	assertRetentionRedacted(t, "expr/selected-order", param.Value().Asc(), secret)
	assertRetentionRedacted(t, "expr/row-comparison", expr.OrderRow(param), secret)
	assertRetentionRedacted(t, "expr/selected-comparison", expr.CompareValue(param.Value()), secret)
	assertRetentionRedacted(t, "expr/group", expr.Group(param), secret)
	assertRetentionRedacted(t, "expr/selected-key", param.Value().Key(), secret)
	assertRetentionRedacted(t, "expr/having", expr.Grouped(projected), secret)
	assertRetentionRedacted(t, "expr/aggregate-filter", agg, secret)
	assertRetentionRedacted(t, "expr/case", expr.When(projected, param), secret)
	assertRetentionRedacted(t, "expr/selected-case", expr.WhenValue(expr.Grouped(projected), param.Value()), secret)
	assertRetentionRedacted(t, "expr/window", window, secret)
	assertRetentionRedacted(t, "expr/window-fallback", expr.LagOr(expr.Row(value).Value(), 1, secret, expr.Window[predicateOwner]{}), secret)
	assertRetentionRedacted(t, "expr/raw-argument", expr.Raw[predicateOwner, string]("?", secret), secret)
	assertRetentionRedacted(t, "expr/raw-sql", expr.Raw[predicateOwner, string]("'"+secret+"'"), secret)
	assertRetentionRedacted(t, "rows/input", source, secret)
	assertRetentionRedacted(t, "rows/scalar", scalar, secret)
	assertRetentionRedacted(t, "rows/value-record", rows.Values(scalar), secret)
	assertRetentionRedacted(t, "rows/value-set", rows.UnionValues(rows.Values(scalar), rows.Values(scalar)), secret)
	assertRetentionRedacted(t, "rows/transaction-value-input", rows.TransactionValueOf(rows.Values(scalar)), secret)
	assertRetentionRedacted(t, "rows/scalar-expression", rows.ScalarRowQuery(source, scalar).Value(), secret)
	assertRetentionRedacted(t, "rows/exists", rows.ExistsQuery(source, scalar), secret)
	assertRetentionRedacted(t, "rows/projection", report, secret)
	assertRetentionRedacted(t, "rows/record", record, secret)
	assertRetentionRedacted(t, "rows/cte", rows.CTE("private_records", record), secret)
	assertRetentionRedacted(t, "rows/set", rows.UnionAll(record, record), secret)
	assertRetentionRedacted(t, "rows/transaction-record", rows.TransactionOf(record), secret)
	assertRetentionRedacted(t, "rows/transaction-subquery", rows.TransactionSubqueryOf(source), secret)
	assertRetentionRedacted(t, "rows/transaction-cte", rows.TransactionCTE("private_records", rows.TransactionOf(record)), secret)
	left, right := rows.As[retentionLeft](q, "l"), rows.As[retentionRight](q, "r")
	lv, rv := expr.At(rows.Scope(left), orm.KeyExpr(table.Key())), expr.At(rows.Scope(right), orm.KeyExpr(table.Key()))
	on := rows.OnEqual(lv, rv).WhereLeft(lv.Eq(secret)).WhereRight(rv.Eq(secret))
	assertRetentionRedacted(t, "rows/alias", left, secret)
	assertRetentionRedacted(t, "rows/join-on", on, secret)
	assertRetentionRedacted(t, "rows/left-predicate", rows.OnEqual(lv, rv).WhereLeft(lv.Eq(secret)), secret)
	assertRetentionRedacted(t, "rows/right-predicate", rows.OnEqual(lv, rv).WhereRight(rv.Eq(secret)), secret)
	assertRetentionRedacted(t, "rows/inner-join", rows.InnerJoin(left, right, on), secret)
	assertRetentionRedacted(t, "rows/left-join", rows.LeftJoin(left, right, on), secret)
	assertRetentionRedacted(t, "rows/right-join", rows.RightJoin(left, right, on), secret)
	assertRetentionRedacted(t, "rows/full-join", rows.FullJoin(left, right, on), secret)
	assertRetentionRedacted(t, "rows/cross-join", rows.CrossJoin(left, right), secret)
	assertRetentionRedacted(t, "rows/correlation", rows.Correlate(left, right), secret)

	assertRetentionRedacted(t, "rows/join-negation", on.Not(), secret)
	assertRetentionRedacted(t, "rows/join-junction", rows.OnAnd(on, on), secret)
	assertRetentionRedacted(t, "rows/annotated-query", rows.WithValue(q, param), secret)
}

func TestWindowOffsetsRetainOpaqueLiterals(t *testing.T) {
	const secret = "945817263"
	assertRetentionRedacted(t, "orm/preceding", expr.Preceding(945817263), secret)
	assertRetentionRedacted(t, "orm/following", expr.Following(945817263), secret)
	assertRetentionRedacted(t, "orm/window-function", expr.NTile(945817263, expr.Window[predicateOwner]{}), secret)
}
