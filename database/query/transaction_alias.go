package query

import "github.com/weiloon1234/Foundry-Go/fault"

// TransactionAliasedSource names a complete record or CTE while retaining the
// transaction requirement and a distinct alias owner for generated fields.
type TransactionAliasedSource[A, R any] struct {
	input  joinInput[TransactionAlias[A, R]]
	record *recordMetadata[R]
}

// AsTransaction gives a transaction-required record source an alias. Locks on a
// source stay in its derived SELECT; CTE references retain their definitions.
func AsTransaction[A, R any](source TransactionRecordSource[R], alias string) TransactionAliasedSource[A, R] {
	record := recordQuery[R]{}
	if nilDescriptor(source) {
		record.err = fault.New(fault.Invalid, "transaction alias requires a record source")
	} else {
		record = source.transactionRecord()
	}
	input, metadata := aliasInput[TransactionAlias[A, R]](record, alias)
	return TransactionAliasedSource[A, R]{input: input, record: metadata}
}

// Scope owns all non-nullable generated fields of this aliased record.
func (a TransactionAliasedSource[A, R]) Scope() ModelScope[TransactionAlias[A, R], R] {
	if a.input.err != nil {
		return ModelScope[TransactionAlias[A, R], R]{}
	}
	return ModelScope[TransactionAlias[A, R], R]{table: a.input.node.source.name(), record: a.record}
}
func (a TransactionAliasedSource[A, R]) transactionProjection() projectionSource[TransactionAlias[A, R]] {
	return projectionSource[TransactionAlias[A, R]]{node: a.input.node, err: a.input.err}
}
