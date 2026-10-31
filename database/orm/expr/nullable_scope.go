package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// NullableScope identifies a possibly absent model in an outer join.
//
// Example: rows.NullableRightScope(joined, rows.Scope(orders))
type NullableScope[S, M any] struct {
	nullablePhase [0]struct{}
	_             [0]*S
	_             [0]*M
	core          ormcore.Scope
}

// AtNullable widens a required column on an outer join's nullable side.
//
// Example: expr.AtNullable(scope, column)
func AtNullable[S, M, V any](scope NullableScope[S, M], column orm.ExpressionColumn[M, V, V]) Expression[S, V, value.Nullable[V]] {
	e := At(Scope[S, M]{core: scope.core}, column)
	c := nullableData(rowData[V]{core: e.core, column: e.projection()})
	e.column.Nullable = true
	return Expression[S, V, value.Nullable[V]]{core: c.core, column: e.column, projection: func() ormcore.Column[value.Nullable[V]] { return c.column }}
}

// AtNullableValue preserves one nullable layer for an already nullable column.
//
// Example: expr.AtNullableValue(scope, column)
func AtNullableValue[S, M, V any](scope NullableScope[S, M], column orm.ExpressionColumn[M, V, value.Nullable[V]]) Expression[S, V, value.Nullable[V]] {
	return At(Scope[S, M]{core: scope.core}, column)
}

// FoundryNullableScope constructs a validated nullable scope.
//
// Example: expr.FoundryNullableScope[User, User](core, seal)
func FoundryNullableScope[S, M any](core ormcore.Scope, seal sqlowner.ExpressionSeal) NullableScope[S, M] {
	if !seal.Valid() {
		return NullableScope[S, M]{core: ormcore.Scope{Err: ormcore.Invalid("invalid SQL owner capability")}}
	}
	return NullableScope[S, M]{core: core}
}

// FoundryNullableScopeData exposes validated nullable metadata.
//
// Example: expr.FoundryNullableScopeData(scope, seal)
func FoundryNullableScopeData[S, M any](scope NullableScope[S, M], seal sqlowner.ExpressionSeal) ormcore.Scope {
	if !seal.Valid() {
		return ormcore.Scope{Err: ormcore.Invalid("invalid SQL owner capability")}
	}
	return scope.core
}
