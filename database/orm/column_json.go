package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSON binds a typed JSON document with sorting but without range comparisons or text patterns.
// Example: orm.JSONEq(Records.Settings, settings).
type JSON[M, T any] struct {
	_    [0]*M
	json columnData[value.JSON[T]]
}

// NullableJSON distinguishes SQL NULL from a typed JSON document, including JSON null.
// Example: orm.NullableJSONIsNull(Records.MaybeSettings).
type NullableJSON[M, T any] struct {
	_            [0]*M
	nullableJSON nullableColumnData[value.JSON[T]]
}

// JSONOf binds immutable typed JSON through its existing codec. Example: JSONOf(b,&m.Settings).
func JSONOf[M, T any](b Builder[M], ptr *value.JSON[T], opts ...ColumnOption[value.JSON[T]]) JSON[M, T] {
	return JSON[M, T]{json: bindModel[M](b.state, ptr, optionCodec(codec.JSON[T](), opts), false, opts...)}
}

// NullableJSONOf binds nullable typed JSON. Example: NullableJSONOf(b,&m.MaybeSettings).
func NullableJSONOf[M, T any](b Builder[M], ptr *value.Nullable[value.JSON[T]], opts ...ColumnOption[value.JSON[T]]) NullableJSON[M, T] {
	return NullableJSON[M, T]{nullableJSON: bindNullableModel[M](b.state, ptr, optionCodec(codec.JSON[T](), opts), opts...)}
}

// JSONEq compares a concrete value. Example: JSONEq(Records.Settings, settings).
func JSONEq[M, T any](c JSON[M, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.Equal, []value.JSON[T]{v})
}

// JSONNe excludes a concrete value. Example: JSONNe(Records.Settings, settings).
func JSONNe[M, T any](c JSON[M, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.NotEqual, []value.JSON[T]{v})
}

// JSONIn tests membership. Example: JSONIn(Records.Settings, a, b).
func JSONIn[M, T any](c JSON[M, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.In, v)
}

// JSONNotIn excludes membership. Example: JSONNotIn(Records.Settings, a, b).
func JSONNotIn[M, T any](c JSON[M, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.json, querycore.NotIn, v)
}

// JSONAsc sorts ascending. Example: JSONAsc(Records.Settings).
func JSONAsc[M, T any](c JSON[M, T]) Order[M] { return columnOrder[M](c.json, false) }

// JSONDesc sorts descending. Example: JSONDesc(Records.Settings).
func JSONDesc[M, T any](c JSON[M, T]) Order[M] { return columnOrder[M](c.json, true) }

// JSONContains tests JSONB document containment, not a text pattern. Example: JSONContains(Records.Settings, settings).
func JSONContains[M, T any](c JSON[M, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.json, querycore.JsonContainsOperation, v)
}

// JSONContainedBy tests the reverse JSONB containment. Example: JSONContainedBy(Records.Settings, settings).
func JSONContainedBy[M, T any](c JSON[M, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.json, querycore.JsonContainedByOperation, v)
}

// JSONIsJSONNull matches a JSON null document, excluding SQL NULL. Example: JSONIsJSONNull(Records.Settings).
func JSONIsJSONNull[M, T any](c JSON[M, T]) Predicate[M] { return jsonNull[M](c.json) }

// JSONSet assigns a valid JSON document; zero value.JSON is rejected without sending SQL.
// Example: JSONSet(Records.Settings, settings).
func JSONSet[M, T any](c JSON[M, T], v value.JSON[T]) Assignment[M] {
	return columnAssignment[M](c.json, v)
}

// NullableJSONEq compares a concrete value. Example: NullableJSONEq(Records.MaybeSettings, settings).
func NullableJSONEq[M, T any](c NullableJSON[M, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.Equal, []value.JSON[T]{v})
}

// NullableJSONNe excludes a concrete value. Example: NullableJSONNe(Records.MaybeSettings, settings).
func NullableJSONNe[M, T any](c NullableJSON[M, T], v value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.NotEqual, []value.JSON[T]{v})
}

// NullableJSONIn tests membership. Example: NullableJSONIn(Records.MaybeSettings, a, b).
func NullableJSONIn[M, T any](c NullableJSON[M, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.In, v)
}

// NullableJSONNotIn excludes membership. Example: NullableJSONNotIn(Records.MaybeSettings, a, b).
func NullableJSONNotIn[M, T any](c NullableJSON[M, T], v ...value.JSON[T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.NotIn, v)
}

// NullableJSONAsc sorts ascending. Example: NullableJSONAsc(Records.MaybeSettings).
func NullableJSONAsc[M, T any](c NullableJSON[M, T]) Order[M] {
	return columnOrder[M](c.nullableJSON.base, false)
}

// NullableJSONDesc sorts descending. Example: NullableJSONDesc(Records.MaybeSettings).
func NullableJSONDesc[M, T any](c NullableJSON[M, T]) Order[M] {
	return columnOrder[M](c.nullableJSON.base, true)
}

// NullableJSONContains excludes SQL NULL and tests JSONB containment. Example: NullableJSONContains(Records.MaybeSettings, settings).
func NullableJSONContains[M, T any](c NullableJSON[M, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableJSON.base, querycore.JsonContainsOperation, v)
}

// NullableJSONContainedBy excludes SQL NULL and tests reverse JSONB containment. Example: NullableJSONContainedBy(Records.MaybeSettings, settings).
func NullableJSONContainedBy[M, T any](c NullableJSON[M, T], v value.JSON[T]) Predicate[M] {
	return jsonContainment[M](c.nullableJSON.base, querycore.JsonContainedByOperation, v)
}

// NullableJSONIsJSONNull matches a JSON null document, excluding SQL NULL. Example: NullableJSONIsJSONNull(Records.MaybeSettings).
func NullableJSONIsJSONNull[M, T any](c NullableJSON[M, T]) Predicate[M] {
	return jsonNull[M](c.nullableJSON.base)
}

// NullableJSONIsNull matches SQL NULL. Example: NullableJSONIsNull(Records.MaybeSettings).
func NullableJSONIsNull[M, T any](c NullableJSON[M, T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.IsNull, nil)
}

// NullableJSONIsNotNull excludes SQL NULL. Example: NullableJSONIsNotNull(Records.MaybeSettings).
func NullableJSONIsNotNull[M, T any](c NullableJSON[M, T]) Predicate[M] {
	return comparison[M](c.nullableJSON.base, querycore.IsNotNull, nil)
}

// NullableJSONSet assigns a valid JSON document; zero value.JSON is rejected without sending SQL.
// Example: NullableJSONSet(Records.MaybeSettings, settings).
func NullableJSONSet[M, T any](c NullableJSON[M, T], v value.JSON[T]) Assignment[M] {
	return columnAssignment[M](c.nullableJSON.base, v)
}

// NullableJSONSetNull explicitly clears this field. Example: NullableJSONSetNull(Records.MaybeSettings).
func NullableJSONSetNull[M, T any](c NullableJSON[M, T]) Assignment[M] {
	return nullAssignment[M](c.nullableJSON.base)
}

func jsonContainment[M, V any](c columnData[V], op querycore.ScalarOperation, v V) Predicate[M] {
	if c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	document, err := c.bind(v)
	return Predicate[M]{owner: c.core.owner, expression: querycore.RetainPredicate(ormcore.JSONContainment(querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, op, document, err))}
}

func jsonNull[M, V any](c columnData[V]) Predicate[M] {
	if c.core == nil || c.core.owner == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	return Predicate[M]{owner: c.core.owner, expression: querycore.RetainPredicate(ormcore.JSONIsNull(querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}))}
}
