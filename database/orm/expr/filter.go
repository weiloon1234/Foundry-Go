package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ModelPredicate adapts a model condition without discarding its declaration owner. Example: expr.ModelPredicate(orm.EnumEq(Users.State, Active)).
func ModelPredicate[M any](p orm.Predicate[M]) Predicate[M] {
	return Predicate[M]{core: orm.FoundryPredicate(p, sqlowner.Metadata())}
}

// Has lifts row filters through a scoped relation EXISTS. Example: expr.Has(UserRelations.Orders,expr.OrderRow(expr.Value(orm.NumberExpr(Orders.Total))).Gt(0)).
func Has[M, N any](r orm.ExistenceRelation[M, N], ps ...Predicate[N]) Predicate[M] {
	inputs := make([]ormcore.Predicate, len(ps))
	for i, p := range ps {
		inputs[i] = p.core
	}
	return ModelPredicate(orm.FoundryRelationPredicate(r, inputs, sqlowner.Metadata()))
}

// RelationFilter adapts expression predicates for a relation's typed target.
// Example: orders.Where(expr.RelationFilter(orders,expr.GreaterColumn(orm.NumberExpr(Orders.Total), Orders.Paid))).
func RelationFilter[M, N any](r orm.ExistenceRelation[M, N], p Predicate[N]) orm.Predicate[N] {
	return orm.FoundryRelationFilter(r, p.core, sqlowner.Metadata())
}

// RowComparison exposes only equality and membership, including for enums.
// Example: expr.CompareRow(expr.Value(orm.EnumExpr(Users.State))).In(Active,Pending).
type RowComparison[S, V any] struct {
	comparisonPhase [0]struct{}
	_               [0]*S
	data            rowData[V]
}

// CompareRow preserves a required column expression without adding ordered or text capabilities. Example: expr.CompareRow(expr.Value(orm.EnumExpr(Users.State))).Eq(Active).
func CompareRow[S, V any](e Expression[S, V, V]) RowComparison[S, V] {
	return RowComparison[S, V]{data: rowData[V]{core: e.core, column: e.column}}
}

// Value selects the computed value. Example: expression.Value().
func (e RowComparison[S, V]) Value() Selected[S, V] { return Selected[S, V]{data: e.data} }

// Eq compares a present value. Example: comparison.Eq(Active).
func (e RowComparison[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, v)}
}

// Ne compares a present value using SQL NULL semantics. Example: comparison.Ne(Active).
func (e RowComparison[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, v)}
}

// In tests membership in present values. Example: comparison.In(Active,Pending).
func (e RowComparison[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, vs...)}
}

// NullableRowComparison retains present-value equality and SQL null tests.
// Example: expr.CompareNullableRow(expr.Value(orm.NullableEnumExpr(Users.OptionalState))).Eq(Active).
type NullableRowComparison[S, V any] struct {
	nullableComparisonPhase [0]struct{}
	_                       [0]*S
	data                    rowData[V]
	projected               rowData[value.Nullable[V]]
}

// CompareNullableRow preserves nullable equality without granting enum ordering or text operations. Example: expr.CompareNullableRow(expr.Value(orm.NullableEnumExpr(Users.OptionalState))).IsNull().
func CompareNullableRow[S, V any](e Expression[S, V, value.Nullable[V]]) NullableRowComparison[S, V] {
	return NullableRowComparison[S, V]{
		data:      rowData[V]{core: e.core, column: e.column},
		projected: e.rowValue(nil),
	}
}

// Value selects the computed value. Example: expression.Value().
func (e NullableRowComparison[S, V]) Value() Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: e.projected}
}

// Eq compares a present value. Example: comparison.Eq(Active).
func (e NullableRowComparison[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.Equal, v)}
}

// Ne compares a present value using SQL NULL semantics. Example: comparison.Ne(Active).
func (e NullableRowComparison[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.NotEqual, v)}
}

// In tests present-value membership. Example: comparison.In(Active,Pending).
func (e NullableRowComparison[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.In, vs...)}
}

// IsNull tests SQL nullness. Example: comparison.IsNull().
func (e NullableRowComparison[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNull)}
}

// IsNotNull tests SQL presence. Example: comparison.IsNotNull().
func (e NullableRowComparison[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(e.data, querycore.IsNotNull)}
}

// TextRow preserves a required text column capability. Example: expr.TextRow(orm.TextExpr(Users.Name)).Contains("Ada").
func TextRow[S any, V ~string](column orm.TextExpressionColumn[S, V, V]) TextRowExpression[S, V] {
	data := checkedTextColumn(rowInput(Value(column)).data)
	return TextRowExpression[S, V]{data: data}
}

// TextNullableRow preserves a nullable text column capability. Example: expr.TextNullableRow(orm.NullableTextExpr(Users.Nickname)).IsNull().
func TextNullableRow[S any, V ~string](column orm.TextExpressionColumn[S, V, value.Nullable[V]]) NullableTextRowExpression[S, V] {
	data := checkedTextColumn(rowInput(Value(column)).data)
	return NullableTextRowExpression[S, V]{data: data}
}

// TextRowAt preserves a required text column in a joined or aliased scope. Example: expr.TextRowAt(memberScope, orm.TextExpr(Members.Name)).Contains("Ada").
func TextRowAt[S, M any, V ~string](scope Scope[S, M], column orm.TextExpressionColumn[M, V, V]) TextRowExpression[S, V] {
	return TextRowExpression[S, V]{data: checkedTextColumn(rowInput(At(scope, column)).data)}
}

// TextNullableRowAt preserves nullable text in a joined or aliased scope. Example: expr.TextNullableRowAt(memberScope, orm.NullableTextExpr(Members.Nickname)).IsNull().
func TextNullableRowAt[S, M any, V ~string](scope Scope[S, M], column orm.TextExpressionColumn[M, V, value.Nullable[V]]) NullableTextRowExpression[S, V] {
	return NullableTextRowExpression[S, V]{data: checkedTextColumn(rowInput(At(scope, column)).data)}
}

func checkedTextColumn[V any](data rowData[V]) rowData[V] {
	data.core.Err = safeerror.Join("operation failed", data.core.Err, data.column.Err, validateTextColumn(data.column.Text))
	data.column.Err = data.core.Err
	return data
}

// Validate the metadata returned by the column, not a marker promoted from a donor.
func validateTextColumn(text bool) error {
	if !text {
		return ormcore.Invalid("text adapter requires a text column")
	}
	return nil
}

// Or combines row conditions without admitting HAVING predicates. Example: expr.Or(expr.Not(note.Eq("x")),note.IsNull()).
func Or[S any](ps ...Predicate[S]) Predicate[S] {
	p := And(ps...)
	if node, ok := p.core.Node.(querycore.Junction); ok {
		node.Any = true
		p.core.Node = node
	}
	return p
}
