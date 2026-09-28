package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
)

// LateralSource is a named correlated record, usable only after its required
// outer input. It cannot enter ordinary joins or execute independently.
type LateralSource[A, O, R any] struct {
	input  joinInput[Alias[A, R]]
	record *recordMetadata[R]
	outer  queryScope[O]
}

// AsLateral names a correlated complete model or declared projection. Use a
// distinct alias tag and name for each occurrence, just as with As.
func AsLateral[A, O, R any](source CorrelatedRecordSource[O, R], alias string) LateralSource[A, O, R] {
	var r correlatedRecord[O, R]
	if nilDescriptor(source) {
		r.query.err = fault.New(fault.Invalid, "lateral alias requires a correlated record and valid name")
	} else {
		r = source.correlatedRecord()
	}
	input, record, outer := lateralInput[Alias[A, R]](r, alias)
	return LateralSource[A, O, R]{input: input, record: record, outer: outer}
}

// Scope exposes the complete lateral record's fields for ON and result selection.
func (s LateralSource[A, O, R]) Scope() RecordScope[Alias[A, R], R] {
	if s.input.err != nil {
		return RecordScope[Alias[A, R], R]{}
	}
	return RecordScope[Alias[A, R], R]{table: s.input.node.source.name(), record: s.record}
}

// CrossJoinLateral evaluates right for each preceding left row. An empty right
// result removes that left row; multiple right records preserve multiplicity.
func CrossJoinLateral[L, A, R any](left JoinInput[L], right LateralSource[A, L, R]) CrossJoined[L, Alias[A, R]] {
	return CrossJoined[L, Alias[A, R]]{buildLateralJoin[Cross[L, Alias[A, R]]](left, right, nil, crossJoin)}
}

// InnerJoinLateral joins correlated records with optional typed ON conditions.
// Omitting conditions means ON TRUE; supplied conditions are combined with AND.
func InnerJoinLateral[L, A, R any](left JoinInput[L], right LateralSource[A, L, R], on ...JoinOn[L, Alias[A, R]]) InnerJoined[L, Alias[A, R]] {
	return InnerJoined[L, Alias[A, R]]{buildLateralJoin[Inner[L, Alias[A, R]]](left, right, on, innerJoin)}
}

// LeftJoinLateral preserves a left row when its correlated result is empty or
// rejected by ON. Use NullableRightScope for the possibly missing right record.
func LeftJoinLateral[L, A, R any](left JoinInput[L], right LateralSource[A, L, R], on ...JoinOn[L, Alias[A, R]]) LeftJoined[L, Alias[A, R]] {
	return LeftJoined[L, Alias[A, R]]{buildLateralJoin[Left[L, Alias[A, R]]](left, right, on, leftJoin)}
}

// The ordinary JoinTable adaptation is private so consumers cannot erase O.
type lateralTable[S any] struct{ input joinInput[S] }

func (t lateralTable[S]) joinTable() joinInput[S] { return t.input }

func buildLateralJoin[S, L, A, R any](left JoinInput[L], right LateralSource[A, L, R], conditions []JoinOn[L, Alias[A, R]], kind joinKind) joinedSource[S, L, Alias[A, R]] {
	return buildLateralInputs[S](left, right.input, right.outer, conditions, kind)
}
