package query

import "github.com/weiloon1234/Foundry-Go/fault"

// TransactionLateralSource is a named correlated record. It can only be joined
// after its declared parent and cannot execute or enter ordinary source APIs.
type TransactionLateralSource[A any, O TransactionScope, R any] struct {
	input  joinInput[TransactionAlias[A, R]]
	record *recordMetadata[R]
	outer  queryScope[O]
}

// AsTransactionLateral names a complete correlated record with its own typed alias.
func AsTransactionLateral[A any, O TransactionScope, R any](source TransactionCorrelatedRecordSource[O, R], alias string) TransactionLateralSource[A, O, R] {
	var r correlatedRecord[O, R]
	if nilDescriptor(source) {
		r.query.err = fault.New(fault.Invalid, "transaction lateral alias requires a correlated record")
	} else {
		r = source.transactionCorrelatedRecord()
	}
	input, record, outer := lateralInput[TransactionAlias[A, R]](r, alias)
	return TransactionLateralSource[A, O, R]{input: input, record: record, outer: outer}
}

// Scope describes the complete lateral result for ON conditions and selection.
func (s TransactionLateralSource[A, O, R]) Scope() RecordScope[TransactionAlias[A, R], R] {
	if s.input.err != nil {
		return RecordScope[TransactionAlias[A, R], R]{}
	}
	return RecordScope[TransactionAlias[A, R], R]{table: s.input.node.source.name(), record: s.record}
}

// TransactionCrossJoinLateral preserves all correlated matches for each parent.
func TransactionCrossJoinLateral[L TransactionScope, A, R any](left TransactionJoinInput[L], right TransactionLateralSource[A, L, R]) TransactionCrossJoined[L, TransactionAlias[A, R]] {
	return TransactionCrossJoined[L, TransactionAlias[A, R]]{buildTransactionLateral[TransactionCross[L, TransactionAlias[A, R]]](left, right, nil, crossJoin)}
}

// TransactionInnerJoinLateral accepts optional typed ON conditions; omission means TRUE.
func TransactionInnerJoinLateral[L TransactionScope, A, R any](left TransactionJoinInput[L], right TransactionLateralSource[A, L, R], on ...JoinOn[L, TransactionAlias[A, R]]) TransactionInnerJoined[L, TransactionAlias[A, R]] {
	return TransactionInnerJoined[L, TransactionAlias[A, R]]{buildTransactionLateral[TransactionInner[L, TransactionAlias[A, R]]](left, right, on, innerJoin)}
}

// TransactionLeftJoinLateral preserves unmatched parents and makes right fields nullable.
func TransactionLeftJoinLateral[L TransactionScope, A, R any](left TransactionJoinInput[L], right TransactionLateralSource[A, L, R], on ...JoinOn[L, TransactionAlias[A, R]]) TransactionLeftJoined[L, TransactionAlias[A, R]] {
	return TransactionLeftJoined[L, TransactionAlias[A, R]]{buildTransactionLateral[TransactionLeft[L, TransactionAlias[A, R]]](left, right, on, leftJoin)}
}
func buildTransactionLateral[S, L TransactionScope, A, R any](left TransactionJoinInput[L], right TransactionLateralSource[A, L, R], on []JoinOn[L, TransactionAlias[A, R]], kind joinKind) transactionJoinedSource[S, L, TransactionAlias[A, R]] {
	var input joinInput[L]
	if nilDescriptor(left) {
		input.err = fault.New(fault.Invalid, "transaction lateral join requires a preceding source")
	} else {
		input = left.transactionJoinInput()
	}
	joined := buildLateralInputs[S](transactionJoinAdapter[L]{input: input}, right.input, right.outer, on, kind)
	return transactionJoinedSource[S, L, TransactionAlias[A, R]]{joined: joined}
}
