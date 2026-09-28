package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/value"
)

type jsonValue interface {
	comparable
	Text() (string, error)
	IsJSONNull() bool
}

// JSONField preserves the exact value.JSON payload type through equality,
// membership, containment, selection and key operations. It does not expose
// numeric aggregates or text operators on the whole document.
type JSONField[M any, V jsonValue] struct{ ScalarField[M, V] }

// NullableJSONField adds SQL NULL without changing the concrete JSON payload.
type NullableJSONField[M any, V jsonValue] struct{ NullableField[M, V] }

func NewJSONField[M any, V jsonValue](table, column string, c codec.Codec[V]) JSONField[M, V] {
	return JSONField[M, V]{NewScalarField[M, V](table, column, c)}
}
func NewNullableJSONField[M any, V jsonValue](table, column string, c codec.Codec[V]) NullableJSONField[M, V] {
	return NullableJSONField[M, V]{NewNullableField[M, V](table, column, c)}
}

// Contains uses PostgreSQL JSONB containment. Array order and repeated elements
// do not affect containment; equality still preserves array order.
func (f JSONField[M, V]) Contains(v V) Predicate[M] {
	return JSONContains(f, f.Param(v)).Eq(true)
}
func (f JSONField[M, V]) ContainedBy(v V) Predicate[M] {
	return JSONContainedBy(f, f.Param(v)).Eq(true)
}
func (f NullableJSONField[M, V]) Contains(v V) Predicate[M] {
	return JSONContainsNullable(f, NullableRow(f.Param(v))).Eq(value.Of(true))
}
func (f NullableJSONField[M, V]) ContainedBy(v V) Predicate[M] {
	return JSONContainedByNullable(f, NullableRow(f.Param(v))).Eq(value.Of(true))
}

// Kind distinguishes the six JSON kinds. JSON null has kind JSONNull.
func (f JSONField[M, V]) Kind() RowExpression[M, JSONKind] { return JSONType(f) }

// Kind preserves SQL NULL separately from the JSONNull kind.
func (f NullableJSONField[M, V]) Kind() RowExpression[M, value.Nullable[JSONKind]] {
	return JSONTypeNullable(f)
}
func (f JSONField[M, V]) IsJSONNull() Predicate[M] { return f.Kind().Eq(JSONNull) }
func (f NullableJSONField[M, V]) IsJSONNull() Predicate[M] {
	return f.Kind().Eq(value.Of(JSONNull))
}
