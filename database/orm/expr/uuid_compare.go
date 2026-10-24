package expr

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func uuidRowComparison[S, V any](input RowValue[S, V], op querycore.Operator, values ...V) Predicate[S] {
	d := rowInput(input).data
	if d.column.Kind != codec.TypeUUID {
		d.core.Err = errors.Join(d.core.Err, ormcore.Invalid("UUID comparison requires a UUID codec"))
	}
	return Predicate[S]{core: rowCompare(d, op, values...)}
}

// UUIDLess retains expression scope and UUID ownership. Example: expr.UUIDLess(expr.At(scope,Files.Key()),after).
func UUIDLess[S, T any](input RowValue[S, model.ID[T]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.Less, v)
}

// UUIDLessNullable compares a present UUID, leaving SQL NULL unmatched. Example: expr.UUIDLessNullable(expr.Value(Files.ParentID),after).
func UUIDLessNullable[S, T any](input RowValue[S, value.Nullable[model.ID[T]]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.Less, value.Of(v))
}

// UUIDLessOrEqual retains expression scope and UUID ownership. Example: expr.UUIDLessOrEqual(expr.At(scope,Files.Key()),after).
func UUIDLessOrEqual[S, T any](input RowValue[S, model.ID[T]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.LessOrEqual, v)
}

// UUIDLessOrEqualNullable compares a present UUID, leaving SQL NULL unmatched. Example: expr.UUIDLessOrEqualNullable(expr.Value(Files.ParentID),after).
func UUIDLessOrEqualNullable[S, T any](input RowValue[S, value.Nullable[model.ID[T]]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.LessOrEqual, value.Of(v))
}

// UUIDGreater retains expression scope and UUID ownership. Example: expr.UUIDGreater(expr.At(scope,Files.Key()),after).
func UUIDGreater[S, T any](input RowValue[S, model.ID[T]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.Greater, v)
}

// UUIDGreaterNullable compares a present UUID, leaving SQL NULL unmatched. Example: expr.UUIDGreaterNullable(expr.Value(Files.ParentID),after).
func UUIDGreaterNullable[S, T any](input RowValue[S, value.Nullable[model.ID[T]]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.Greater, value.Of(v))
}

// UUIDGreaterOrEqual retains expression scope and UUID ownership. Example: expr.UUIDGreaterOrEqual(expr.At(scope,Files.Key()),after).
func UUIDGreaterOrEqual[S, T any](input RowValue[S, model.ID[T]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.GreaterOrEqual, v)
}

// UUIDGreaterOrEqualNullable compares a present UUID, leaving SQL NULL unmatched. Example: expr.UUIDGreaterOrEqualNullable(expr.Value(Files.ParentID),after).
func UUIDGreaterOrEqualNullable[S, T any](input RowValue[S, value.Nullable[model.ID[T]]], v model.ID[T]) Predicate[S] {
	return uuidRowComparison(input, querycore.GreaterOrEqual, value.Of(v))
}
