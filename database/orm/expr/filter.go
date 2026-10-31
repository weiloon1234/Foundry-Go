package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ModelPredicate adapts a model condition without discarding its declaration owner.
//
// Example: expr.ModelPredicate(p)
func ModelPredicate[M any](p orm.Predicate[M]) Predicate[M] {
	return Predicate[M]{core: orm.FoundryPredicate(p, sqlowner.Metadata())}
}

// Has lifts row filters through a scoped relation EXISTS.
//
// Example: expr.Has(r, ps...)
func Has[M, N any](r orm.ExistenceRelation[M, N], ps ...Predicate[N]) Predicate[M] {
	inputs := make([]ormcore.Predicate, len(ps))
	for i, p := range ps {
		inputs[i] = p.core
	}
	return ModelPredicate(orm.FoundryRelationPredicate(r, inputs, sqlowner.Metadata()))
}

// RelationFilter adapts expression predicates for a relation's typed target.
//
// Example: expr.RelationFilter(r, p)
func RelationFilter[M, N any](r orm.ExistenceRelation[M, N], p Predicate[N]) orm.Predicate[N] {
	return orm.FoundryRelationFilter(r, p.core, sqlowner.Metadata())
}

// RowComparison exposes only equality and membership, including for enums.
//
// Example: expr.In(expr.CompareRow(expr.Value(orm.EnumExpr(Users.State))), Active, Pending)
type RowComparison[S, V any] = rowComparison[*S, V]

type rowComparison[S, V any] struct {
	comparisonPhase [0]struct{}
	_               [0]S
	data            rowData[V]
}

// CompareRow preserves a required column expression without adding ordered or text capabilities.
//
// Example: expr.CompareRow(e)
func CompareRow[S, V any](e Expression[S, V, V]) RowComparison[S, V] {
	return RowComparison[S, V]{data: rowData[V]{core: e.core, column: e.column}}
}

func (e rowComparison[S, V]) rowValue(S) rowData[V] { return e.data }

// NullableRowComparison retains present-value equality and SQL null tests.
//
// Example: expr.NullableEq(expr.CompareNullableRow(expr.Value(orm.NullableEnumExpr(Users.OptionalState))), Active)
type NullableRowComparison[S, V any] = nullableRowComparison[*S, V]

type nullableRowComparison[S, V any] struct {
	nullableComparisonPhase [0]struct{}
	_                       [0]S
	data                    rowData[V]
	projected               rowData[value.Nullable[V]]
}

// CompareNullableRow preserves nullable equality without granting enum ordering or text operations.
//
// Example: expr.CompareNullableRow(e)
func CompareNullableRow[S, V any](e Expression[S, V, value.Nullable[V]]) NullableRowComparison[S, V] {
	return NullableRowComparison[S, V]{
		data:      rowData[V]{core: e.core, column: e.column},
		projected: e.rowValue(nil),
	}
}

func (e nullableRowComparison[S, V]) rowValue(S) rowData[value.Nullable[V]] { return e.projected }

// TextFilterAdapter adapts a text row expression for datatable filters.
//
// Example: expr.TextFilter(name)
type TextFilterAdapter[S any, V ~string] struct {
	textFilterPhase [0]struct{}
	_               [0]*S
	data            rowData[V]
}

// Eq compares equality within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares inequality within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In tests membership within the adapter's row scope.
func (a TextFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Lt compares less than within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Less, v)}
}

// Lte compares less than or equal within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, v)}
}

// Gt compares greater than within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Greater, v)}
}

// Gte compares greater than or equal within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, v)}
}

// Contains tests a text substring within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Contains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Contains, v)}
}

// Like tests a SQL text pattern within the adapter's row scope.
func (a TextFilterAdapter[S, V]) Like(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Like, v)}
}

// IContains tests a text substring without case sensitivity within the adapter's row scope.
func (a TextFilterAdapter[S, V]) IContains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.InsensitiveContains, v)}
}

// NullableTextFilterAdapter adapts a nullable text row expression for datatable filters.
//
// Example: expr.NullableTextFilter(nickname)
type NullableTextFilterAdapter[S any, V ~string] struct {
	nullableTextFilterPhase [0]struct{}
	_                       [0]*S
	data                    rowData[value.Nullable[V]]
}

// Eq compares equality within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares inequality within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In tests membership within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, presentValues(vs)...)}
}

// Lt compares less than within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares less than or equal within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares greater than within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares greater than or equal within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// Contains tests a text substring within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Contains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Contains, presentValues([]V{v})...)}
}

// Like tests a SQL text pattern within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) Like(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Like, presentValues([]V{v})...)}
}

// IContains tests a text substring without case sensitivity within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) IContains(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.InsensitiveContains, presentValues([]V{v})...)}
}

// IsNull tests SQL nullness within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests SQL presence within the adapter's row scope.
func (a NullableTextFilterAdapter[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// OrderFilterAdapter adapts an ordered row expression for datatable filters.
//
// Example: expr.OrderFilter(age)
type OrderFilterAdapter[S any, V orderedScalar] struct {
	orderFilterPhase [0]struct{}
	_                [0]*S
	data             rowData[V]
}

// Eq compares equality within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares inequality within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In tests membership within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// Lt compares less than within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Less, v)}
}

// Lte compares less than or equal within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, v)}
}

// Gt compares greater than within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Greater, v)}
}

// Gte compares greater than or equal within the adapter's row scope.
func (a OrderFilterAdapter[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, v)}
}

// NullableOrderFilterAdapter adapts a nullable ordered row expression for datatable filters.
//
// Example: expr.NullableOrderFilter(age)
type NullableOrderFilterAdapter[S any, V orderedScalar] struct {
	nullableOrderFilterPhase [0]struct{}
	_                        [0]*S
	data                     rowData[value.Nullable[V]]
}

// Eq compares equality within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, presentValues([]V{v})...)}
}

// Ne compares inequality within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, presentValues([]V{v})...)}
}

// In tests membership within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, presentValues(vs)...)}
}

// Lt compares less than within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Lt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Less, presentValues([]V{v})...)}
}

// Lte compares less than or equal within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Lte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.LessOrEqual, presentValues([]V{v})...)}
}

// Gt compares greater than within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Gt(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Greater, presentValues([]V{v})...)}
}

// Gte compares greater than or equal within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) Gte(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.GreaterOrEqual, presentValues([]V{v})...)}
}

// IsNull tests SQL nullness within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests SQL presence within the adapter's row scope.
func (a NullableOrderFilterAdapter[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// CompareFilterAdapter adapts a row comparison for datatable filters.
//
// Example: expr.CompareFilter(expr.CompareRow(state))
type CompareFilterAdapter[S, V any] struct {
	comparisonFilterPhase [0]struct{}
	_                     [0]*S
	data                  rowData[V]
}

// Eq compares equality within the adapter's row scope.
func (a CompareFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares inequality within the adapter's row scope.
func (a CompareFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In tests membership within the adapter's row scope.
func (a CompareFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// NullableCompareFilterAdapter adapts a nullable row comparison for datatable filters.
//
// Example: expr.NullableCompareFilter(expr.CompareNullableRow(state))
type NullableCompareFilterAdapter[S, V any] struct {
	nullableComparisonFilterPhase [0]struct{}
	_                             [0]*S
	data                          rowData[V]
	projected                     rowData[value.Nullable[V]]
}

// Eq compares equality within the adapter's row scope.
func (a NullableCompareFilterAdapter[S, V]) Eq(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.Equal, v)}
}

// Ne compares inequality within the adapter's row scope.
func (a NullableCompareFilterAdapter[S, V]) Ne(v V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.NotEqual, v)}
}

// In tests membership within the adapter's row scope.
func (a NullableCompareFilterAdapter[S, V]) In(vs ...V) Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.In, vs...)}
}

// IsNull tests SQL nullness within the adapter's row scope.
func (a NullableCompareFilterAdapter[S, V]) IsNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNull)}
}

// IsNotNull tests SQL presence within the adapter's row scope.
func (a NullableCompareFilterAdapter[S, V]) IsNotNull() Predicate[S] {
	return Predicate[S]{core: rowCompare(a.data, querycore.IsNotNull)}
}

// TextFilter adapts TextRowExpression for datatable filters.
//
// Example: expr.TextFilter(e)
func TextFilter[S any, V ~string](e TextRowExpression[S, V]) TextFilterAdapter[S, V] {
	return TextFilterAdapter[S, V]{data: e.data}
}

// NullableTextFilter adapts NullableTextRowExpression for datatable filters.
//
// Example: expr.NullableTextFilter(e)
func NullableTextFilter[S any, V ~string](e NullableTextRowExpression[S, V]) NullableTextFilterAdapter[S, V] {
	return NullableTextFilterAdapter[S, V]{data: e.data}
}

// OrderFilter adapts OrderedRowExpression for datatable filters.
//
// Example: expr.OrderFilter(e)
func OrderFilter[S any, V orderedScalar](e OrderedRowExpression[S, V]) OrderFilterAdapter[S, V] {
	return OrderFilterAdapter[S, V]{data: e.data}
}

// NullableOrderFilter adapts NullableOrderedRowExpression for datatable filters.
//
// Example: expr.NullableOrderFilter(e)
func NullableOrderFilter[S any, V orderedScalar](e NullableOrderedRowExpression[S, V]) NullableOrderFilterAdapter[S, V] {
	return NullableOrderFilterAdapter[S, V]{data: e.data}
}

// CompareFilter adapts RowComparison for datatable filters.
//
// Example: expr.CompareFilter(e)
func CompareFilter[S, V any](e RowComparison[S, V]) CompareFilterAdapter[S, V] {
	return CompareFilterAdapter[S, V]{data: e.data}
}

// NullableCompareFilter adapts NullableRowComparison for datatable filters.
//
// Example: expr.NullableCompareFilter(e)
func NullableCompareFilter[S, V any](e NullableRowComparison[S, V]) NullableCompareFilterAdapter[S, V] {
	return NullableCompareFilterAdapter[S, V]{data: e.data, projected: e.projected}
}

// TextRow preserves a required text column capability.
//
// Example: expr.TextRow(column)
func TextRow[S any, V ~string](column orm.TextExpressionColumn[S, V, V]) TextRowExpression[S, V] {
	data := checkedTextColumn(rowInput(Value(column)).data)
	return TextRowExpression[S, V]{data: data}
}

// TextNullableRow preserves a nullable text column capability.
//
// Example: expr.TextNullableRow(column)
func TextNullableRow[S any, V ~string](column orm.TextExpressionColumn[S, V, value.Nullable[V]]) NullableTextRowExpression[S, V] {
	data := checkedTextColumn(rowInput(Value(column)).data)
	return NullableTextRowExpression[S, V]{data: data}
}

// TextRowAt preserves a required text column in a joined or aliased scope.
//
// Example: expr.TextRowAt(scope, column)
func TextRowAt[S, M any, V ~string](scope Scope[S, M], column orm.TextExpressionColumn[M, V, V]) TextRowExpression[S, V] {
	return TextRowExpression[S, V]{data: checkedTextColumn(rowInput(At(scope, column)).data)}
}

// TextNullableRowAt preserves nullable text in a joined or aliased scope.
//
// Example: expr.TextNullableRowAt(scope, column)
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

// Or combines row conditions without admitting HAVING predicates.
//
// Example: expr.Or(ps...)
func Or[S any](ps ...Predicate[S]) Predicate[S] {
	p := And(ps...)
	if node, ok := p.core.Node.(querycore.Junction); ok {
		node.Any = true
		p.core.Node = node
	}
	return p
}
