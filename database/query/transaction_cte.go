package query

import "github.com/weiloon1234/Foundry-Go/fault"

// TransactionCommonTable is a reusable complete-record CTE whose consumers
// require an active transaction. Its SELECT retains its own row-lock clauses.
type TransactionCommonTable[R any] struct{ definition CommonTable[R] }

// TransactionCTE names a transaction-required complete record query. Locked model
// and result queries implement TransactionRecordSource; ordinary inputs use
// TransactionOf. Dependencies share the existing CTE planner.
func TransactionCTE[R any](name string, source TransactionRecordSource[R]) TransactionCommonTable[R] {
	record := recordQuery[R]{}
	if nilDescriptor(source) {
		record.err = fault.New(fault.Invalid, "transaction CTE requires a record source")
	} else {
		record = source.transactionRecord()
	}
	return TransactionCommonTable[R]{definition: CTE(name, transactionRecordAdapter[R]{record: record})}
}

// Materialized requests PostgreSQL materialization while retaining all locks.
func (c TransactionCommonTable[R]) Materialized() TransactionCommonTable[R] {
	c.definition = c.definition.Materialized()
	return c
}

// NotMaterialized requests inlining where PostgreSQL permits it. PostgreSQL
// can ignore this hint for a SELECT with row locks; the locks are never removed.
func (c TransactionCommonTable[R]) NotMaterialized() TransactionCommonTable[R] {
	c.definition = c.definition.NotMaterialized()
	return c
}
func (c TransactionCommonTable[R]) transactionRecord() recordQuery[R] {
	return c.definition.recordQuery()
}
