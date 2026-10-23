package expr

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Coalesce removes nullability with a required fallback of the same owner/type.
// Example: expr.Coalesce(expr.Value(Users.Nickname),expr.Value(Users.Name)).
func Coalesce[S, V any](first RowValue[S, value.Nullable[V]], fallback RowValue[S, V]) RowExpression[S, V] {
	return RowExpression[S, V]{data: CoalesceValue(Select(first), Select(fallback)).data}
}

// CoalesceValue composes selected values with a required fallback.
// Example: expr.CoalesceValue(expr.Nullable(expr.Count[User]().Value()),expr.Count[User]().Param(0).Value()).
func CoalesceValue[S, V any](first Selected[S, value.Nullable[V]], fallback Selected[S, V]) Selected[S, V] {
	out := conditionalData(querycore.CoalesceValues, fallback.data.column, first.data.core, fallback.data.core)
	if value.IsNullableType[V]() {
		out.core.Err = errors.Join(out.core.Err, ormcore.Invalid("non-null fallback must not be nullable"))
	}
	return Selected[S, V]{data: out}
}

// CoalesceNullable retains one nullable layer when all inputs may be NULL.
// Example: expr.CoalesceNullable(expr.Value(Users.Nickname),expr.NullFor(expr.Value(Users.Name))).
func CoalesceNullable[S, V any](first RowValue[S, value.Nullable[V]], rest ...RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	initial := rowInput(first).data
	if len(rest) >= querycore.MaxExpressionNodes {
		initial.core.Err = errors.Join(initial.core.Err, ormcore.Invalid("COALESCE exceeds its argument bound"))
		return RowExpression[S, value.Nullable[V]]{data: initial}
	}
	arguments := make([]ormcore.Expression, 1, len(rest)+1)
	arguments[0] = initial.core
	for _, item := range rest {
		arguments = append(arguments, rowInput(item).data.core)
	}
	return RowExpression[S, value.Nullable[V]]{data: conditionalData(querycore.CoalesceValues, initial.column, arguments...)}
}

// CoalesceNullableValue combines selected nullable inputs without nesting wrappers.
// Example: expr.CoalesceNullableValue(expr.Nullable(expr.Count[User]().Value())).
func CoalesceNullableValue[S, V any](first Selected[S, value.Nullable[V]], rest ...Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	if len(rest) >= querycore.MaxExpressionNodes {
		first.data.core.Err = errors.Join(first.data.core.Err, ormcore.Invalid("COALESCE exceeds its argument bound"))
		return first
	}
	arguments := make([]ormcore.Expression, 1, len(rest)+1)
	arguments[0] = first.data.core
	for _, item := range rest {
		arguments = append(arguments, item.data.core)
	}
	return Selected[S, value.Nullable[V]]{data: conditionalData(querycore.CoalesceValues, first.data.column, arguments...)}
}

// NullIf adds one nullable layer to matching required row values.
// Example: expr.NullIf(expr.Value(Users.Name),expr.Param(Users.Name,"")).
func NullIf[S, V any](first, second RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{data: NullIfValue(Select(first), Select(second)).data}
}

// NullIfValue adds one nullable layer to matching required selected values.
// Example: expr.NullIfValue(expr.Count[User]().Value(),expr.Count[User]().Param(0).Value()).
func NullIfValue[S, V any](first, second Selected[S, V]) Selected[S, value.Nullable[V]] {
	data := nullableData(first.data)
	return Selected[S, value.Nullable[V]]{data: conditionalData(querycore.NullIfValues, data.column, data.core, second.data.core)}
}

// NullIfNullable compares nullable row values while retaining one nullable layer.
// Example: expr.NullIfNullable(expr.Value(Users.Nickname),expr.NullFor(expr.Value(Users.Name))).
func NullIfNullable[S, V any](first, second RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{data: NullIfNullableValue(Select(first), Select(second)).data}
}

// NullIfNullableValue compares selected nullable values without nesting wrappers.
// Example: expr.NullIfNullableValue(a,b).
func NullIfNullableValue[S, V any](first, second Selected[S, value.Nullable[V]]) Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: conditionalData(querycore.NullIfValues, first.data.column, first.data.core, second.data.core)}
}

func conditionalData[V any](kind querycore.ConditionalKind, column ormcore.Column[V], arguments ...ormcore.Expression) rowData[V] {
	core := ormcore.Conditional(kind, arguments...)
	core.Err = errors.Join(core.Err, column.Err)
	return rowData[V]{core: core, column: column}
}

// Case is unfinished row CASE; Else or ElseNull must finish it before use.
// Example: expr.When(age.Gt(18),name).Else(fallback).
type Case[S, V any] struct {
	rowCasePhase [0]struct{}
	_            [0]*S
	builder      ormcore.CaseBuilder
	column       ormcore.Column[V]
}

// ValueCase is unfinished selected CASE with group predicates and selected results.
// Example: expr.WhenValue(count.Gt(0),count.Value()).Else(count.Param(0).Value()).
type ValueCase[S, V any] struct {
	selectedCasePhase [0]struct{}
	_                 [0]*S
	builder           ormcore.CaseBuilder
	column            ormcore.Column[V]
}

// When starts row CASE with a same-owner row condition and result.
// Example: expr.When(age.Gt(18),name).
func When[S, V any](condition Predicate[S], result RowValue[S, V]) Case[S, V] {
	return (Case[S, V]{}).When(condition, result)
}

// When appends an independent row CASE branch with the exact result type.
// Example: choice.When(age.Gt(65),seniorName).
func (c Case[S, V]) When(condition Predicate[S], result RowValue[S, V]) Case[S, V] {
	data := rowInput(result).data
	if c.builder.Empty() {
		c.column = data.column
	}
	c.builder = c.builder.Append(condition.core, data.core)
	return c
}

// Else finishes row CASE with a matching fallback.
// Example: choice.Else(name).
func (c Case[S, V]) Else(result RowValue[S, V]) RowExpression[S, V] {
	data := rowInput(result).data
	return RowExpression[S, V]{data: caseData(c.builder, c.column, data.core)}
}

// ElseNull finishes required-valued row CASE with one nullable result layer.
// Example: expr.When(age.Gt(18),name).ElseNull().
func (c Case[S, V]) ElseNull() RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{data: caseNullData(c.builder, c.column)}
}

// WhenValue starts selected CASE using group predicates and selected results.
// Example: expr.WhenValue(count.Gt(0),count.Value()).
func WhenValue[S, V any](condition HavingPredicate[S], result Selected[S, V]) ValueCase[S, V] {
	return (ValueCase[S, V]{}).When(condition, result)
}

// When appends an independent selected CASE branch.
// Example: choice.When(count.Gt(10),count.Value()).
func (c ValueCase[S, V]) When(condition HavingPredicate[S], result Selected[S, V]) ValueCase[S, V] {
	if c.builder.Empty() {
		c.column = result.data.column
	}
	c.builder = c.builder.Append(condition.core, result.data.core)
	return c
}

// Else finishes selected CASE with a matching selected fallback.
// Example: choice.Else(count.Param(0).Value()).
func (c ValueCase[S, V]) Else(result Selected[S, V]) Selected[S, V] {
	return Selected[S, V]{data: caseData(c.builder, c.column, result.data.core)}
}

// ElseNull finishes required-valued selected CASE with one nullable layer.
// Example: expr.WhenValue(count.Gt(0),count.Value()).ElseNull().
func (c ValueCase[S, V]) ElseNull() Selected[S, value.Nullable[V]] {
	return Selected[S, value.Nullable[V]]{data: caseNullData(c.builder, c.column)}
}

// Grouped lifts a row predicate into the group phase; SQL grouping still applies.
// Example: expr.WhenValue(expr.Grouped(age.Gt(18)),count.Value()).
func Grouped[S any](condition Predicate[S]) HavingPredicate[S] {
	return HavingPredicate[S]{core: condition.core}
}

func caseData[V any](builder ormcore.CaseBuilder, column ormcore.Column[V], otherwise ormcore.Expression) rowData[V] {
	core := builder.Finish(otherwise)
	core.Err = errors.Join(core.Err, column.Err)
	return rowData[V]{core: core, column: column}
}
func caseNullData[V any](builder ormcore.CaseBuilder, column ormcore.Column[V]) rowData[value.Nullable[V]] {
	data := nullableData(rowData[V]{column: column})
	otherwise := ormcore.Expression{Node: querycore.ParameterNode{Kind: column.Kind}, ReferenceFree: true, Err: data.core.Err}
	return caseData(builder, data.column, otherwise)
}
