package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// CommonTable is a reusable, complete, read-only record definition.
// Example: rows.AsRecord[tag](rows.CTE("active",records),"a").
type CommonTable[R any] struct {
	_           [0]*R
	ordinaryCTE [0]struct{}
	core        ormcore.CommonTable
}

func (c CommonTable[R]) recordData(*R) ormcore.RecordSource { return ormcore.CommonTableRecord(c.core) }

// CTE names a complete model or projection; dependencies are emitted once per statement.
// Example: rows.CTE("active",rows.ModelRecord(Users.Query().Where(orm.ColumnEq(Users.Active, true)))).
func CTE[R any](name string, source RecordSource[R]) CommonTable[R] {
	return CommonTable[R]{core: ormcore.NewCommonTable(name, recordOf(source))}
}

// Materialized requests explicit PostgreSQL materialization. Example: cte.Materialized().
func (c CommonTable[R]) Materialized() CommonTable[R] {
	c.core = ormcore.Materialize(c.core, querycore.CteMaterialized)
	return c
}

// NotMaterialized requests inlining; recursive definitions reject it. Example: cte.NotMaterialized().
func (c CommonTable[R]) NotMaterialized() CommonTable[R] {
	c.core = ormcore.Materialize(c.core, querycore.CteNotMaterialized)
	return c
}

// TransactionCommonTable retains a complete record's transaction requirement and locks.
// Example: rows.AsTransaction[tag](cte,"claim").
type TransactionCommonTable[R any] struct {
	_              [0]*R
	transactionCTE [0]struct{}
	core           ormcore.CommonTable
}

func (c TransactionCommonTable[R]) transactionRecord(*R) ormcore.RecordSource {
	return ormcore.CommonTableRecord(c.core)
}

// TransactionCTE names a transaction-required complete record.
// Example: rows.TransactionCTE("claimed",rows.LockedRecord(orm.Lock(Orders.Query()))).
func TransactionCTE[R any](name string, source TransactionRecordSource[R]) TransactionCommonTable[R] {
	return TransactionCommonTable[R]{core: ormcore.NewCommonTable(name, transactionRecordOf(source))}
}

// Materialized retains the executor requirement. Example: cte.Materialized().
func (c TransactionCommonTable[R]) Materialized() TransactionCommonTable[R] {
	c.core = ormcore.Materialize(c.core, querycore.CteMaterialized)
	return c
}

// NotMaterialized retains all locks even when PostgreSQL ignores the hint. Example: cte.NotMaterialized().
func (c TransactionCommonTable[R]) NotMaterialized() TransactionCommonTable[R] {
	c.core = ormcore.Materialize(c.core, querycore.CteNotMaterialized)
	return c
}
