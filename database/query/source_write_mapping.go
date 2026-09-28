package query

import (
	"fmt"

	"github.com/weiloon1234/Foundry-Go/value"
)

// SourceKey matches a source SQL value to the destination model's primary key.
// Matching a key never assigns it or invokes its getter or mutator.
type SourceKey[S, M any] struct {
	_ [0]*S
	_ [0]*M
	modelValueMapping
	nullable bool
}

// MatchSource identifies a destination by a compatible stored primary value.
// The write rejects non-primary destination fields before starting a transaction.
func MatchSource[S, M, V any](field ModelValueField[M, V], expression Expression[S, V]) SourceKey[S, M] {
	return SourceKey[S, M]{modelValueMapping: captureModelMapping(field, expression)}
}

// MatchNullableSource accepts an outer join's nullable key. NULL matches no model.
func MatchNullableSource[S, M, V any](field ModelValueField[M, V], expression Expression[S, value.Nullable[V]]) SourceKey[S, M] {
	// Capture the destination's stored type while retaining the nullable SQL node.
	key := MatchSource(field, Expression[S, V]{node: expression.node})
	key.nullable = true
	return key
}

func (SourceKey[S, M]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model source key"))
}

// UpdateMapping assigns a stored source expression to a non-primary model field.
// Use literal draft inputs for fields with Go mutators.
type UpdateMapping[S, M any] struct {
	_ [0]*S
	_ [0]*M
	modelValueMapping
}

// MapUpdate retains the exact stored field type, including SQL NULL semantics.
func MapUpdate[S, M, V any](field ModelValueField[M, V], expression Expression[S, V]) UpdateMapping[S, M] {
	return UpdateMapping[S, M]{modelValueMapping: captureModelMapping(field, expression)}
}

func (UpdateMapping[S, M]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model update mapping"))
}
