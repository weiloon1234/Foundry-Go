package expr

import (
	"github.com/weiloon1234/Foundry-Go/value"
)

// TextValueComparison supplies selected text conditions for HAVING.
//
// Example: expr.TextValue(selectedName)
type TextValueComparison[S any, V ~string] = textValueComparison[*S, V]

type textValueComparison[S any, V ~string] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[V]
}

// NullableTextValueComparison retains NULL and compares present text in HAVING.
//
// Example: expr.TextNullableValue(selectedName)
type NullableTextValueComparison[S any, V ~string] = nullableTextValueComparison[*S, V]

type nullableTextValueComparison[S any, V ~string] struct {
	selectedPhase [0]struct{}
	_             [0]S
	data          rowData[value.Nullable[V]]
}

// TextValue exposes selected text comparisons.
//
// Example: expr.TextValue(v)
func TextValue[S any, V ~string](v Selected[S, V]) TextValueComparison[S, V] {
	return TextValueComparison[S, V]{data: v.data}
}

// TextNullableValue exposes nullable selected text comparisons.
//
// Example: expr.TextNullableValue(v)
func TextNullableValue[S any, V ~string](v Selected[S, value.Nullable[V]]) NullableTextValueComparison[S, V] {
	return NullableTextValueComparison[S, V]{data: v.data}
}
