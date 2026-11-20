package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

// JSON binds a typed JSON document with sorting but without range comparisons or text patterns.
// Example: orm.Eq(Records.Settings, settings).
type JSON[M, T any] = ColumnHandle[M, value.JSON[T], value.JSON[T], value.JSON[T], RequiredJSONKind, OrdinaryRole]

// NullableJSON distinguishes SQL NULL from a typed JSON document, including JSON null.
// Example: orm.IsNull(Records.MaybeSettings).
type NullableJSON[M, T any] = ColumnHandle[M, value.JSON[T], value.JSON[T], value.Nullable[value.JSON[T]], NullableJSONKind, OrdinaryRole]

// JSONOf binds immutable typed JSON through its existing codec. Example: JSONOf(b,&m.Settings).
func JSONOf[M, T any](b Builder[M], ptr *value.JSON[T], opts ...ColumnOption[value.JSON[T]]) JSON[M, T] {
	c := bindModel[M](b.state, ptr, optionCodec(codec.JSON[T](), opts), false, opts...)
	return JSON[M, T]{stored: c, input: c.assign}
}

// NullableJSONOf binds nullable typed JSON. Example: NullableJSONOf(b,&m.MaybeSettings).
func NullableJSONOf[M, T any](b Builder[M], ptr *value.Nullable[value.JSON[T]], opts ...ColumnOption[value.JSON[T]]) NullableJSON[M, T] {
	c := bindNullableModel[M](b.state, ptr, optionCodec(codec.JSON[T](), opts), opts...)
	return NullableJSON[M, T]{stored: c, input: c.assign}
}

func jsonContainment[M, V any](c *columnData[V], op querycore.ScalarOperation, v V) Predicate[M] {
	if c == nil || c.core == nil || c.core.owner == nil || c.bind == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	document, err := c.bind(v)
	return Predicate[M]{owner: c.core.owner, expression: querycore.RetainPredicate(ormcore.JSONContainment(querycore.FieldRef{Table: c.core.owner.model.Table, Column: c.core.metadata.Name}, op, document, err))}
}

func jsonNull[M any](core *columnCore) Predicate[M] {
	if core == nil || core.owner == nil {
		return Predicate[M]{err: invalid("uninitialized JSON column")}
	}
	return Predicate[M]{owner: core.owner, expression: querycore.RetainPredicate(ormcore.JSONIsNull(querycore.FieldRef{Table: core.owner.model.Table, Column: core.metadata.Name}))}
}
