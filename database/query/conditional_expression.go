package query

import (
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type conditionalKind uint8

const (
	coalesceValues conditionalKind = iota + 1
	nullIfValues
)

type conditionalNode struct {
	kind      conditionalKind
	arguments []valueExpression
	err       error
}

func (conditionalNode) valueNode() {}

type caseBranch struct {
	condition expression
	result    valueExpression
}
type caseNode struct {
	branches  []caseBranch
	otherwise valueExpression
	err       error
}

func (caseNode) valueNode() {}

// Coalesce returns a non-NULL fallback when the first row value is SQL NULL.
// A zero/empty value is retained. Owners and concrete value types must match.
func Coalesce[S, V any](first RowValue[S, value.Nullable[V]], fallback RowValue[S, V]) RowExpression[S, V] {
	return RowExpression[S, V]{CoalesceValue(rowInput(first).Value(), rowInput(fallback).Value())}
}

// CoalesceValue is the selected-value counterpart, accepting aggregate/window
// results as well as row values. Its result cannot become a row predicate.
func CoalesceValue[S, V any](first Expression[S, value.Nullable[V]], fallback Expression[S, V]) Expression[S, V] {
	n := conditionalNode{kind: coalesceValues, arguments: []valueExpression{first.node, fallback.node}}
	if value.IsNullableType[V]() {
		n.err = fault.New(fault.Invalid, "non-null fallback must not be nullable")
	}
	return Expression[S, V]{node: n, codec: fallback.codec}
}

// CoalesceNullable returns the first non-NULL row value, or SQL NULL when all
// inputs are NULL. Use Coalesce for a statically non-nullable final fallback.
func CoalesceNullable[S, V any](first RowValue[S, value.Nullable[V]], rest ...RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	items := make([]Expression[S, value.Nullable[V]], len(rest))
	for i, item := range rest {
		items[i] = rowInput(item).Value()
	}
	return RowExpression[S, value.Nullable[V]]{CoalesceNullableValue(rowInput(first).Value(), items...)}
}

// CoalesceNullableValue accepts selected nullable inputs and retains one nullable layer.
func CoalesceNullableValue[S, V any](first Expression[S, value.Nullable[V]], rest ...Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	n := conditionalNode{kind: coalesceValues, arguments: []valueExpression{first.node}}
	for _, item := range rest {
		n.arguments = append(n.arguments, item.node)
	}
	first.node = n
	return first
}

// NullIf returns SQL NULL when two non-nullable row values compare equal.
// Their concrete types must match. Database equality follows its collation.
func NullIf[S, V any](first, second RowValue[S, V]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{NullIfValue(rowInput(first).Value(), rowInput(second).Value())}
}

// NullIfValue compares selected values and adds one nullable result layer.
func NullIfValue[S, V any](first, second Expression[S, V]) Expression[S, value.Nullable[V]] {
	n := conditionalNode{kind: nullIfValues, arguments: []valueExpression{first.node, second.node}}
	if value.IsNullableType[V]() {
		n.err = fault.New(fault.Invalid, "nullable NULLIF input requires NullIfNullable")
	}
	return Expression[S, value.Nullable[V]]{node: n, codec: codec.Nullable(first.codec)}
}

// NullIfNullable preserves one nullable layer while comparing nullable inputs.
func NullIfNullable[S, V any](first, second RowValue[S, value.Nullable[V]]) RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{NullIfNullableValue(rowInput(first).Value(), rowInput(second).Value())}
}

// NullIfNullableValue compares selected nullable inputs without nesting wrappers.
func NullIfNullableValue[S, V any](first, second Expression[S, value.Nullable[V]]) Expression[S, value.Nullable[V]] {
	first.node = conditionalNode{kind: nullIfValues, arguments: []valueExpression{first.node, second.node}}
	return first
}

// Case is an unfinished, typed row CASE. At least one When and an explicit
// Else/ElseNull are required before it can be selected or used in a predicate.
type Case[S, V any] struct{ builder caseBuilder[S, V] }

// ValueCase is a selected CASE whose conditions/results may contain ordinary
// aggregates. It never exposes row predicate methods.
type ValueCase[S, V any] struct{ builder caseBuilder[S, V] }
type caseBuilder[S, V any] struct {
	_        [0]*S
	branches []caseBranch
	codec    codec.Codec[V]
}

// When begins a row CASE; only the first true branch supplies its value.
func When[S, V any](condition Predicate[S], result RowValue[S, V]) Case[S, V] {
	return (Case[S, V]{}).When(condition, result)
}
func (c Case[S, V]) When(condition Predicate[S], result RowValue[S, V]) Case[S, V] {
	c.builder = c.builder.append(condition.expression, rowInput(result).Value())
	return c
}
func (c Case[S, V]) Else(result RowValue[S, V]) RowExpression[S, V] {
	return RowExpression[S, V]{c.builder.finish(rowInput(result).Value().node)}
}
func (c Case[S, V]) ElseNull() RowExpression[S, value.Nullable[V]] {
	return RowExpression[S, value.Nullable[V]]{c.builder.finishNull()}
}

// WhenValue begins a selected CASE. Use Grouped to explicitly lift a row
// condition; grouping rules still apply to every referenced row column.
func WhenValue[S, V any](condition HavingPredicate[S], result Expression[S, V]) ValueCase[S, V] {
	return (ValueCase[S, V]{}).When(condition, result)
}
func (c ValueCase[S, V]) When(condition HavingPredicate[S], result Expression[S, V]) ValueCase[S, V] {
	c.builder = c.builder.append(condition.expression, result)
	return c
}
func (c ValueCase[S, V]) Else(result Expression[S, V]) Expression[S, V] {
	return c.builder.finish(result.node)
}
func (c ValueCase[S, V]) ElseNull() Expression[S, value.Nullable[V]] { return c.builder.finishNull() }

func (c caseBuilder[S, V]) append(condition expression, result Expression[S, V]) caseBuilder[S, V] {
	if len(c.branches) == 0 {
		c.codec = result.codec
	}
	c.branches = append(slices.Clone(c.branches), caseBranch{condition, result.node})
	return c
}
func (c caseBuilder[S, V]) finish(otherwise valueExpression) Expression[S, V] {
	return Expression[S, V]{node: caseNode{branches: c.branches, otherwise: otherwise}, codec: c.codec}
}
func (c caseBuilder[S, V]) finishNull() Expression[S, value.Nullable[V]] {
	n := caseNode{branches: c.branches, otherwise: parameterNode{kind: c.codec.ParameterType()}}
	if value.IsNullableType[V]() {
		n.err = fault.New(fault.Invalid, "CASE result is already nullable; provide a nullable Else value")
	}
	return Expression[S, value.Nullable[V]]{node: n, codec: codec.Nullable(c.codec)}
}
