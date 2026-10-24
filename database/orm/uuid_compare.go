package orm

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
)

// UUID comparisons use PostgreSQL's bytewise UUID order, not UUID generation time.
// Nullable columns compare present values; SQL NULL does not match a range.
func uuidColumn[M, T any](column KeyColumn[M, model.ID[T]]) (columnData[model.ID[T]], error) {
	if nilInterface(column) {
		return columnData[model.ID[T]]{}, invalid("UUID comparison requires a column")
	}
	c := column.ormColumn(nil)
	if c.core == nil || c.core.kind != codec.TypeUUID {
		return c, invalid("UUID comparison requires a UUID codec")
	}
	return c, nil
}
func uuidComparison[M, T any](column KeyColumn[M, model.ID[T]], op querycore.Operator, values ...model.ID[T]) Predicate[M] {
	c, err := uuidColumn(column)
	if err != nil {
		return Predicate[M]{err: err}
	}
	return comparison[M](c, op, values)
}

// UUIDLess compares model-owned UUIDs bytewise. Example: orm.UUIDLess(Files.Key(),after).
func UUIDLess[M, T any](column KeyColumn[M, model.ID[T]], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.Less, v)
}

// UUIDLessOrEqual compares model-owned UUIDs bytewise. Example: orm.UUIDLessOrEqual(Files.Key(),after).
func UUIDLessOrEqual[M, T any](column KeyColumn[M, model.ID[T]], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.LessOrEqual, v)
}

// UUIDGreater compares model-owned UUIDs bytewise. Example: orm.UUIDGreater(Files.Key(),after).
func UUIDGreater[M, T any](column KeyColumn[M, model.ID[T]], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.Greater, v)
}

// UUIDGreaterOrEqual compares model-owned UUIDs bytewise. Example: orm.UUIDGreaterOrEqual(Files.Key(),after).
func UUIDGreaterOrEqual[M, T any](column KeyColumn[M, model.ID[T]], v model.ID[T]) Predicate[M] {
	return uuidComparison(column, querycore.GreaterOrEqual, v)
}
