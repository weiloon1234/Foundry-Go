package query

import "github.com/weiloon1234/Foundry-Go/fault"

// TransactionJoinInput is an alias or joined input whose consumers require Tx.
type TransactionJoinInput[S TransactionScope] interface{ transactionJoinInput() joinInput[S] }

// TransactionJoinTable is one aliased transaction-required record appended to a join.
type TransactionJoinTable[S TransactionScope] interface{ transactionJoinTable() joinInput[S] }

type transactionJoinAdapter[S TransactionScope] struct{ input joinInput[S] }

func (a transactionJoinAdapter[S]) joinInput() joinInput[S] { return a.input }
func (a transactionJoinAdapter[S]) joinTable() joinInput[S] { return a.input }

// Only transaction interfaces are promoted by the public wrappers below.
// The ordinary joinedSource remains a named private field.
type transactionJoinedSource[S, L, R TransactionScope] struct{ joined joinedSource[S, L, R] }

func (j transactionJoinedSource[S, L, R]) transactionJoinInput() joinInput[S] { return j.joined.input }
func (j transactionJoinedSource[S, L, R]) transactionProjection() projectionSource[S] {
	return projectionSource[S]{node: j.joined.input.node, err: j.joined.input.err}
}
func (j transactionJoinedSource[S, L, R]) leftScope() scopeBoundary[S, L] {
	return j.joined.leftScope()
}
func (j transactionJoinedSource[S, L, R]) rightScope() scopeBoundary[S, R] {
	return j.joined.rightScope()
}

func buildTransactionJoin[S, L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R], on JoinOn[L, R], kind joinKind) transactionJoinedSource[S, L, R] {
	var result transactionJoinedSource[S, L, R]
	if nilDescriptor(left) || nilDescriptor(right) {
		result.joined.input.err = fault.New(fault.Invalid, "transaction join requires both sources")
		return result
	}
	result.joined = buildJoin[S](transactionJoinAdapter[L]{input: left.transactionJoinInput()}, transactionJoinAdapter[R]{input: right.transactionJoinTable()}, on, kind)
	return result
}

func (a TransactionAliasedSource[A, R]) transactionJoinInput() joinInput[TransactionAlias[A, R]] {
	return a.input
}
func (a TransactionAliasedSource[A, R]) transactionJoinTable() joinInput[TransactionAlias[A, R]] {
	return a.input
}

// TransactionInner identifies a transaction-required inner join scope.
type TransactionInner[L, R TransactionScope] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ transactionInner bool }
}

func (TransactionInner[L, R]) transactionScope() {}

// TransactionInnerJoined preserves both inputs while retaining every nested lock.
type TransactionInnerJoined[L, R TransactionScope] struct {
	transactionJoinedSource[TransactionInner[L, R], L, R]
}

// TransactionInnerJoin preserves both inputs. Existing filters and locks keep their SELECT boundaries.
func TransactionInnerJoin[L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionInnerJoined[L, R] {
	return TransactionInnerJoined[L, R]{buildTransactionJoin[TransactionInner[L, R]](left, right, on, innerJoin)}
}

// TransactionLeft identifies a transaction-required left join scope.
type TransactionLeft[L, R TransactionScope] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ transactionLeft bool }
}

func (TransactionLeft[L, R]) transactionScope() {}

// TransactionLeftJoined makes the right input nullable while retaining every nested lock.
type TransactionLeftJoined[L, R TransactionScope] struct {
	transactionJoinedSource[TransactionLeft[L, R], L, R]
}

// TransactionLeftJoin makes the right input nullable. Existing filters and locks keep their SELECT boundaries.
func TransactionLeftJoin[L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionLeftJoined[L, R] {
	return TransactionLeftJoined[L, R]{buildTransactionJoin[TransactionLeft[L, R]](left, right, on, leftJoin)}
}

// TransactionRight identifies a transaction-required right join scope.
type TransactionRight[L, R TransactionScope] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ transactionRight bool }
}

func (TransactionRight[L, R]) transactionScope() {}

// TransactionRightJoined makes the preceding left inputs nullable while retaining every nested lock.
type TransactionRightJoined[L, R TransactionScope] struct {
	transactionJoinedSource[TransactionRight[L, R], L, R]
}

// TransactionRightJoin makes the preceding left inputs nullable. Existing filters and locks keep their SELECT boundaries.
func TransactionRightJoin[L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionRightJoined[L, R] {
	return TransactionRightJoined[L, R]{buildTransactionJoin[TransactionRight[L, R]](left, right, on, rightJoin)}
}

// TransactionFull identifies a transaction-required full join scope.
type TransactionFull[L, R TransactionScope] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ transactionFull bool }
}

func (TransactionFull[L, R]) transactionScope() {}

// TransactionFullJoined makes both unmatched inputs nullable while retaining every nested lock.
type TransactionFullJoined[L, R TransactionScope] struct {
	transactionJoinedSource[TransactionFull[L, R], L, R]
}

// TransactionFullJoin makes both unmatched inputs nullable. Existing filters and locks keep their SELECT boundaries.
func TransactionFullJoin[L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionFullJoined[L, R] {
	return TransactionFullJoined[L, R]{buildTransactionJoin[TransactionFull[L, R]](left, right, on, fullJoin)}
}

// TransactionCross identifies a transaction-required cross join scope.
type TransactionCross[L, R TransactionScope] struct {
	_ [0]*L
	_ [0]*R
	_ [0]struct{ transactionCross bool }
}

func (TransactionCross[L, R]) transactionScope() {}

// TransactionCrossJoined contains every pair of input rows while retaining every nested lock.
type TransactionCrossJoined[L, R TransactionScope] struct {
	transactionJoinedSource[TransactionCross[L, R], L, R]
}

// TransactionCrossJoin contains every pair of input rows. Existing filters and locks keep their SELECT boundaries.
func TransactionCrossJoin[L, R TransactionScope](left TransactionJoinInput[L], right TransactionJoinTable[R]) TransactionCrossJoined[L, R] {
	return TransactionCrossJoined[L, R]{buildTransactionJoin[TransactionCross[L, R]](left, right, JoinOn[L, R]{}, crossJoin)}
}
