package expr

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
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
func AtNullable[S, M, V, I, P any, K orm.RequiredColumnKind, R orm.ColumnRole](scope NullableScope[S, M], column orm.ColumnHandle[M, V, I, P, K, R]) Expression[S, V, value.Nullable[V]] {
	c, err := scopedComparison(Scope[S, M]{core: scope.core}, column)
	baseCore := ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: err}
	widened := nullableData(rowData[V]{core: baseCore, column: c})
	c.Nullable = true
	return Expression[S, V, value.Nullable[V]]{core: widened.core, column: c, projection: func() ormcore.Column[value.Nullable[V]] { return widened.column }}
}

// AtNullableValue preserves one nullable layer for an already nullable column.
//
// Example: expr.AtNullableValue(scope, column)
func AtNullableValue[S, M, V, I, P any, K orm.NullableComparableKind, R orm.ColumnRole](scope NullableScope[S, M], column orm.ColumnHandle[M, V, I, P, K, R]) Expression[S, V, value.Nullable[V]] {
	c, projected := orm.FoundryNullableHandle(column, sqlowner.Metadata())
	err := safeerror.Join("operation failed", c.Err, scope.core.Err)
	if scope.core.Owner == nil || scope.core.Owner != c.Owner || scope.core.Table == "" {
		err = safeerror.Join("operation failed", err, ormcore.Invalid("column belongs to a different model scope"))
	}
	c.Field.Table = scope.core.Table
	c.Reference = scope.core.Reference
	projected.Reference = scope.core.Reference
	for _, output := range scope.core.Columns {
		if output.Name == c.Field.Column {
			c.Collatable = !ormcore.MergeNonCollatable(!c.Collatable, output.NonCollatable)
			projected.Collatable = !ormcore.MergeNonCollatable(!projected.Collatable, output.NonCollatable)
			break
		}
	}
	return Expression[S, V, value.Nullable[V]]{column: c, projection: func() ormcore.Column[value.Nullable[V]] { return projected }, core: ormcore.Expression{Node: c.Field, Refs: []ormcore.Reference{c.Reference}, Err: err}}
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
