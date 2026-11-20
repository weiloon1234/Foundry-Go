package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
)

// UUID comparisons use PostgreSQL's bytewise UUID order, not UUID generation time.
// Nullable columns compare present values; SQL NULL does not match a range.
func uuidComparison[M, T, I, P any, K ColumnKind, R ColumnRole](column ColumnHandle[M, model.ID[T], I, P, K, R], op querycore.Operator, values ...model.ID[T]) Predicate[M] {
	c := handleData(column)
	if c == nil || c.core == nil || c.core.kind != codec.TypeUUID {
		return Predicate[M]{err: invalid("UUID comparison requires a UUID codec")}
	}
	return comparison[M](c, op, values, false)
}

// UUIDLess compares model-owned UUIDs bytewise. Example: orm.UUIDLess(Files.Key(), after).
func UUIDLess[M, T, I, P any, K ColumnKind, R ColumnRole](column ColumnHandle[M, model.ID[T], I, P, K, R], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.Less, v)
}

// UUIDLessOrEqual compares model-owned UUIDs bytewise. Example: orm.UUIDLessOrEqual(Files.Key(), after).
func UUIDLessOrEqual[M, T, I, P any, K ColumnKind, R ColumnRole](column ColumnHandle[M, model.ID[T], I, P, K, R], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.LessOrEqual, v)
}

// UUIDGreater compares model-owned UUIDs bytewise. Example: orm.UUIDGreater(Files.Key(), after).
func UUIDGreater[M, T, I, P any, K ColumnKind, R ColumnRole](column ColumnHandle[M, model.ID[T], I, P, K, R], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.Greater, v)
}

// UUIDGreaterOrEqual compares model-owned UUIDs bytewise. Example: orm.UUIDGreaterOrEqual(Files.Key(), after).
func UUIDGreaterOrEqual[M, T, I, P any, K ColumnKind, R ColumnRole](column ColumnHandle[M, model.ID[T], I, P, K, R], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.GreaterOrEqual, v)
}
