package expr

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// SelectSource is a sealed row or aggregate value that can enter the selected phase.
//
// Example: var source expr.SelectSource[User, int64] = expr.Count[User]()
type SelectSource[S, V any] interface{ selectValue(*S) rowData[V] }

// ParamSource supplies a scoped codec for an exact parameter type.
//
// Example: var source expr.ParamSource[User, int64] = expr.Count[User]()
type ParamSource[S, V any] interface{ parameterValue(*S) rowData[V] }

// ValueOperand supplies literal comparisons in HAVING, retaining scope and value types.
// Nullable operands compare present values using their nullable codec.
//
// Example: var source expr.ValueOperand[User, int64] = expr.Count[User]()
type ValueOperand[S, V any] interface {
	compareValue(*S, querycore.Operator, ...V) ormcore.Predicate
}

// OrderedValueOperand supplies ordered literal comparisons in HAVING.
//
// Example: var source expr.OrderedValueOperand[User, int64] = expr.Count[User]()
type OrderedValueOperand[S, V any] interface {
	ValueOperand[S, V]
	orderedValueOperand(*S)
}

// TextValueOperand supplies text comparisons in HAVING without losing the declared string type.
//
// Example: var source expr.TextValueOperand[User, string] = expr.TextValue(selectedName)
type TextValueOperand[S any, V ~string] interface {
	OrderedValueOperand[S, V]
	textValueOperand(*S)
}

// NullValueOperand supplies SQL null tests in HAVING.
//
// Example: var source expr.NullValueOperand[Order, decimal.Decimal] = expr.Sum(orm.NumberExpr(Orders.Total))
type NullValueOperand[S, V any] interface {
	ValueOperand[S, V]
	compareValueNull(*S, querycore.Operator) ormcore.Predicate
}

// ValueOrderSource is a selected or aggregate value that can supply selected ordering.
//
// Example: var source expr.ValueOrderSource[User, int64] = expr.Count[User]()
type ValueOrderSource[S, V any] interface {
	ParamSource[S, V]
	valueOrder(*S, bool) ormcore.Order
}

// FilterableAggregate preserves the concrete aggregate type when adding row filters.
//
// Example: var source expr.FilterableAggregate[User, expr.OrderedAggregate[User, int64]] = expr.Count[User]()
type FilterableAggregate[S any, A any] interface{ filterAggregate(*S, []Predicate[S]) A }

// AsSelected promotes a row or aggregate result to the selected phase, preserving nullability.
//
// Example: expr.AsSelected(expr.Count[User]())
func AsSelected[S, V any](source SelectSource[S, V]) Selected[S, V] {
	if source == nil {
		return Selected[S, V]{data: rowData[V]{core: ormcore.Expression{Err: ormcore.Invalid("missing expression")}}}
	}
	return Selected[S, V]{data: source.selectValue(nil)}
}

// ParamOf snapshots a bound value using a scoped expression's codec without evaluating it.
// A nullable aggregate supplies its present-value codec for fallback parameters.
//
// Example: expr.ParamOf(expr.Count[User](), int64(0))
func ParamOf[S, V any](source ParamSource[S, V], v V) RowExpression[S, V] {
	if source == nil {
		return RowExpression[S, V]{data: rowData[V]{core: ormcore.Expression{Err: ormcore.Invalid("missing parameter source")}}}
	}
	return parameter[S](source.parameterValue(nil), v)
}

// Filter adds same-scope row predicates to one aggregate and retains its concrete result type.
//
// Example: expr.Filter(expr.Count[User](), expr.Eq(age, 18))
func Filter[S any, A FilterableAggregate[S, A]](a A, predicates ...Predicate[S]) A {
	return a.filterAggregate(nil, predicates)
}

// Key constructs a selected key while preserving source ownership.
//
// Example: expr.Key(expr.AsSelected(expr.Count[User]()))
func Key[S, V any](e Selected[S, V]) ProjectionKey[S] { return ProjectionKey[S]{core: e.data.core} }

// AscValue orders a selected or aggregate value ascending in a rows query.
//
// Example: expr.AscValue(expr.Count[User]()).NullsFirst()
func AscValue[S, V any](source ValueOrderSource[S, V]) SelectedOrder[S] {
	if source == nil {
		return SelectedOrder[S]{core: ormcore.Order{Err: ormcore.Invalid("missing selected order")}}
	}
	return SelectedOrder[S]{core: source.valueOrder(nil, false)}
}

// DescValue orders a selected or aggregate value descending in a rows query.
//
// Example: expr.DescValue(expr.Count[User]()).NullsLast()
func DescValue[S, V any](source ValueOrderSource[S, V]) SelectedOrder[S] {
	if source == nil {
		return SelectedOrder[S]{core: ormcore.Order{Err: ormcore.Invalid("missing selected order")}}
	}
	return SelectedOrder[S]{core: source.valueOrder(nil, true)}
}

func (e rowExpression[S, V]) selectValue(S) rowData[V] { return e.data }

func (e orderedRowExpression[S, V]) selectValue(S) rowData[V] { return e.data }

func (e nullableOrderedRowExpression[S, V]) selectValue(S) rowData[value.Nullable[V]] { return e.data }

func (e textRowExpression[S, V]) selectValue(S) rowData[V] { return e.data }

func (e nullableTextRowExpression[S, V]) selectValue(S) rowData[value.Nullable[V]] { return e.data }

func (e rowComparison[S, V]) selectValue(S) rowData[V] { return e.data }

func (e nullableRowComparison[S, V]) selectValue(S) rowData[value.Nullable[V]] { return e.projected }

func (a aggregate[S, V]) selectValue(S) rowData[V] { return a.data }

func (a orderedAggregate[S, V]) selectValue(S) rowData[V] { return a.data }

func (a nullableOrderedAggregate[S, V]) selectValue(S) rowData[value.Nullable[V]] { return a.data }

func (e rowExpression[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e orderedRowExpression[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e nullableOrderedRowExpression[S, V]) parameterValue(S) rowData[value.Nullable[V]] {
	return e.data
}

func (e textRowExpression[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e nullableTextRowExpression[S, V]) parameterValue(S) rowData[value.Nullable[V]] { return e.data }

func (e selected[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e aggregate[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e orderedAggregate[S, V]) parameterValue(S) rowData[V] { return e.data }

func (e nullableOrderedAggregate[S, V]) parameterValue(S) rowData[V] {
	return rowData[V]{core: e.data.core, column: e.present}
}

func (a aggregate[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, vs...)
}

func (a orderedAggregate[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, vs...)
}

func (a orderedAggregate[S, V]) orderedValueOperand(S) {}

func (a nullableOrderedAggregate[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, presentValues(vs)...)
}

func (a nullableOrderedAggregate[S, V]) orderedValueOperand(S) {}

func (a nullableOrderedAggregate[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a valueComparison[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, vs...)
}

func (a valueComparison[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a orderedValueComparison[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, vs...)
}

func (a orderedValueComparison[S, V]) orderedValueOperand(S) {}

func (a orderedValueComparison[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a nullableOrderedValueComparison[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, presentValues(vs)...)
}

func (a nullableOrderedValueComparison[S, V]) orderedValueOperand(S) {}

func (a nullableOrderedValueComparison[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a textValueComparison[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, vs...)
}

func (a textValueComparison[S, V]) orderedValueOperand(S) {}

func (a textValueComparison[S, V]) textValueOperand(S) {}

func (a textValueComparison[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a nullableTextValueComparison[S, V]) compareValue(_ S, op querycore.Operator, vs ...V) ormcore.Predicate {
	return rowCompare(a.data, op, presentValues(vs)...)
}

func (a nullableTextValueComparison[S, V]) orderedValueOperand(S) {}

func (a nullableTextValueComparison[S, V]) textValueOperand(S) {}

func (a nullableTextValueComparison[S, V]) compareValueNull(_ S, op querycore.Operator) ormcore.Predicate {
	return rowCompare(a.data, op)
}

func (a aggregate[S, V]) valueOrder(_ S, descending bool) ormcore.Order {
	return ormcore.OrderExpression(a.data.core, descending)
}

func (a orderedAggregate[S, V]) valueOrder(_ S, descending bool) ormcore.Order {
	return ormcore.OrderExpression(a.data.core, descending)
}

func (a nullableOrderedAggregate[S, V]) valueOrder(_ S, descending bool) ormcore.Order {
	return ormcore.OrderExpression(a.data.core, descending)
}

func (a selected[S, V]) valueOrder(_ S, descending bool) ormcore.Order {
	return ormcore.OrderExpression(a.data.core, descending)
}

func (a aggregate[S, V]) filterAggregate(_ S, ps []predicate[S]) aggregate[S, V] {
	a.data.core = filterAggregatePointer(a.data.core, ps)
	return a
}

func (a orderedAggregate[S, V]) filterAggregate(_ S, ps []predicate[S]) orderedAggregate[S, V] {
	a.data.core = filterAggregatePointer(a.data.core, ps)
	return a
}

func (a nullableOrderedAggregate[S, V]) filterAggregate(_ S, ps []predicate[S]) nullableOrderedAggregate[S, V] {
	a.data.core = filterAggregatePointer(a.data.core, ps)
	return a
}

// EqValue tests equality in HAVING using the operand's exact comparison type.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.EqValue(expr.Count[User](), int64(1))
func EqValue[S, V any](source ValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.Equal, v)
}

// NeValue tests inequality in HAVING using the operand's exact comparison type.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.NeValue(expr.Count[User](), int64(1))
func NeValue[S, V any](source ValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.NotEqual, v)
}

// InValue tests membership in HAVING using the operand's exact comparison type.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.InValue(expr.Count[User](), int64(1))
func InValue[S, V any](source ValueOperand[S, V], vs ...V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.In, vs...)
}

// LtValue tests whether a selected value is less than its bound in HAVING.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.LtValue(expr.Count[User](), int64(1))
func LtValue[S, V any](source OrderedValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.Less, v)
}

// LteValue tests whether a selected value is less than or equal to its bound in HAVING.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.LteValue(expr.Count[User](), int64(1))
func LteValue[S, V any](source OrderedValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.LessOrEqual, v)
}

// GtValue tests whether a selected value is greater than its bound in HAVING.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.GtValue(expr.Count[User](), int64(1))
func GtValue[S, V any](source OrderedValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.Greater, v)
}

// GteValue tests whether a selected value is greater than or equal to its bound in HAVING.
// Nullable operands bind present values without changing their SQL null semantics.
//
// Example: expr.GteValue(expr.Count[User](), int64(1))
func GteValue[S, V any](source OrderedValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.GreaterOrEqual, v)
}

// IsNullValue tests SQL nullness in HAVING.
//
// Example: expr.IsNullValue(expr.Sum(orm.NumberExpr(Orders.Total)))
func IsNullValue[S, V any](source NullValueOperand[S, V]) HavingPredicate[S] {
	return compareSelectedNull(source, querycore.IsNull)
}

// IsNotNullValue tests SQL presence in HAVING.
//
// Example: expr.IsNotNullValue(expr.Sum(orm.NumberExpr(Orders.Total)))
func IsNotNullValue[S, V any](source NullValueOperand[S, V]) HavingPredicate[S] {
	return compareSelectedNull(source, querycore.IsNotNull)
}

// TextLikeValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextLikeValue(expr.TextValue(selectedName), "a")
func TextLikeValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.Like, v)
}

// TextILikeValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextILikeValue(expr.TextValue(selectedName), "a")
func TextILikeValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.InsensitiveLike, v)
}

// TextContainsValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextContainsValue(expr.TextValue(selectedName), "a")
func TextContainsValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.Contains, v)
}

// TextIContainsValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextIContainsValue(expr.TextValue(selectedName), "a")
func TextIContainsValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.InsensitiveContains, v)
}

// TextStartsWithValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextStartsWithValue(expr.TextValue(selectedName), "a")
func TextStartsWithValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.StartsWith, v)
}

// TextEndsWithValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextEndsWithValue(expr.TextValue(selectedName), "a")
func TextEndsWithValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.EndsWith, v)
}

// TextIStartsWithValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextIStartsWithValue(expr.TextValue(selectedName), "a")
func TextIStartsWithValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.InsensitiveStartsWith, v)
}

// TextIEndsWithValue compares selected text in HAVING with the database collation.
// Nullable operands compare present text while preserving SQL null semantics.
//
// Example: expr.TextIEndsWithValue(expr.TextValue(selectedName), "a")
func TextIEndsWithValue[S any, V ~string](source TextValueOperand[S, V], v V) HavingPredicate[S] {
	return compareSelectedValue(source, querycore.InsensitiveEndsWith, v)
}

func compareSelectedValue[S, V any](source ValueOperand[S, V], operator querycore.Operator, values ...V) HavingPredicate[S] {
	if source == nil {
		return HavingPredicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("missing selected comparison")}}
	}
	return HavingPredicate[S]{core: source.compareValue(nil, operator, values...)}
}

func compareSelectedNull[S, V any](source NullValueOperand[S, V], operator querycore.Operator) HavingPredicate[S] {
	if source == nil {
		return HavingPredicate[S]{core: ormcore.Predicate{Err: ormcore.Invalid("missing selected comparison")}}
	}
	return HavingPredicate[S]{core: source.compareValueNull(nil, operator)}
}
