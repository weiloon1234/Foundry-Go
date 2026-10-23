package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSON binds a typed JSON document with sorting but without range comparisons or text patterns.
// Example: Records.Settings.Eq(settings).
type JSON[M, T any] struct {
	_    [0]*M
	json columnData[value.JSON[T]]
}

// NullableJSON distinguishes SQL NULL from a typed JSON document, including JSON null.
// Example: Records.MaybeSettings.IsNull().
type NullableJSON[M, T any] struct {
	_            [0]*M
	nullableJSON nullableColumnData[value.JSON[T]]
}

// JSONOf binds immutable typed JSON through its existing codec. Example: JSONOf(b,&m.Settings).
func JSONOf[M, T any](b Builder[M], ptr *value.JSON[T]) JSON[M, T] {
	return JSON[M, T]{json: bindTyped(b.state, ptr, codec.JSON[T](), false)}
}

// NullableJSONOf binds nullable typed JSON. Example: NullableJSONOf(b,&m.MaybeSettings).
func NullableJSONOf[M, T any](b Builder[M], ptr *value.Nullable[value.JSON[T]]) NullableJSON[M, T] {
	return NullableJSON[M, T]{nullableJSON: bindNullable(b.state, ptr, codec.JSON[T]())}
}

// Eq compares a concrete value. Example: Records.Settings.Eq(settings).
func (c JSON[M, T]) Eq(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.Equal, []value.JSON[T]{v})
}

// Ne excludes a concrete value. Example: Records.Settings.Ne(settings).
func (c JSON[M, T]) Ne(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.NotEqual, []value.JSON[T]{v})
}

// In tests membership. Example: Records.Settings.In(a, b).
func (c JSON[M, T]) In(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.In, v)
}

// NotIn excludes membership. Example: Records.Settings.NotIn(a, b).
func (c JSON[M, T]) NotIn(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Records.Settings.Asc().
func (c JSON[M, T]) Asc() Order[M] { return columnOrder[M](c.json, false) }

// Desc sorts descending. Example: Records.Settings.Desc().
func (c JSON[M, T]) Desc() Order[M] { return columnOrder[M](c.json, true) }

// Contains tests JSONB document containment, not a text pattern. Example: Records.Settings.Contains(settings).
func (c JSON[M, T]) Contains(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.json, querycore.JsonContainsOperation, v)
}

// ContainedBy tests the reverse JSONB containment. Example: Records.Settings.ContainedBy(settings).
func (c JSON[M, T]) ContainedBy(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.json, querycore.JsonContainedByOperation, v)
}

// IsJSONNull matches a JSON null document, excluding SQL NULL. Example: Records.Settings.IsJSONNull().
func (c JSON[M, T]) IsJSONNull() Predicate[M] { return jsonNull[M](c.json) }

// Set assigns a valid JSON document; zero value.JSON is rejected without sending SQL.
// Example: Records.Settings.Set(settings).
func (c JSON[M, T]) Set(v value.JSON[T]) Assignment[M] { return columnAssignment[M](c.json, v) }

func (c JSON[M, T]) ormColumn(*M) columnData[value.JSON[T]] { return c.json }

func (c JSON[M, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.JSON[T]]) {
	return requiredExpression(c.json)
}

// Eq compares a concrete value. Example: Records.MaybeSettings.Eq(settings).
func (c NullableJSON[M, T]) Eq(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.Equal, []value.JSON[T]{v})
}

// Ne excludes a concrete value. Example: Records.MaybeSettings.Ne(settings).
func (c NullableJSON[M, T]) Ne(v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.NotEqual, []value.JSON[T]{v})
}

// In tests membership. Example: Records.MaybeSettings.In(a, b).
func (c NullableJSON[M, T]) In(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.In, v)
}

// NotIn excludes membership. Example: Records.MaybeSettings.NotIn(a, b).
func (c NullableJSON[M, T]) NotIn(v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.NotIn, v)
}

// Asc sorts ascending. Example: Records.MaybeSettings.Asc().
func (c NullableJSON[M, T]) Asc() Order[M] { return columnOrder[M](c.nullableJSON.base, false) }

// Desc sorts descending. Example: Records.MaybeSettings.Desc().
func (c NullableJSON[M, T]) Desc() Order[M] { return columnOrder[M](c.nullableJSON.base, true) }

// Contains excludes SQL NULL and tests JSONB containment. Example: Records.MaybeSettings.Contains(settings).
func (c NullableJSON[M, T]) Contains(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableJSON.base, querycore.JsonContainsOperation, v)
}

// ContainedBy excludes SQL NULL and tests reverse JSONB containment. Example: Records.MaybeSettings.ContainedBy(settings).
func (c NullableJSON[M, T]) ContainedBy(v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableJSON.base, querycore.JsonContainedByOperation, v)
}

// IsJSONNull matches a JSON null document, excluding SQL NULL. Example: Records.MaybeSettings.IsJSONNull().
func (c NullableJSON[M, T]) IsJSONNull() Predicate[M] { return jsonNull[M](c.nullableJSON.base) }

// IsNull matches SQL NULL. Example: Records.MaybeSettings.IsNull().
func (c NullableJSON[M, T]) IsNull() Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.IsNull, nil)
}

// IsNotNull excludes SQL NULL. Example: Records.MaybeSettings.IsNotNull().
func (c NullableJSON[M, T]) IsNotNull() Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.IsNotNull, nil)
}

// Set assigns a valid JSON document; zero value.JSON is rejected without sending SQL.
// Example: Records.MaybeSettings.Set(settings).
func (c NullableJSON[M, T]) Set(v value.JSON[T]) Assignment[M] {
	return columnAssignment[M](c.nullableJSON.base, v)
}

// SetNull explicitly clears this field. Example: Records.MaybeSettings.SetNull().
func (c NullableJSON[M, T]) SetNull() Assignment[M] {
	return nullAssignment[M](c.nullableJSON.base)
}

func (c NullableJSON[M, T]) ormColumn(*M) columnData[value.JSON[T]] { return c.nullableJSON.base }

func (c NullableJSON[M, T]) expressionColumn(*M) (ormcore.Column[value.JSON[T]], ormcore.Column[value.Nullable[value.JSON[T]]]) {
	return nullableExpression(c.nullableJSON)
}

func jsonContainment[M, V any](c columnData[V], op querycore.ScalarOperation, v V) Predicate[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	document, err := c.bind(v)
	return Predicate[M]{owner: c.core.owner, expression: ormcore.JSONContainment(querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, op, document, err)}
}

func jsonNull[M, V any](c columnData[V]) Predicate[M] {
	if c.core == nil || c.core.owner == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	return Predicate[M]{owner: c.core.owner, expression: ormcore.JSONIsNull(querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name})}
}
