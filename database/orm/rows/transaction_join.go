package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// TransactionScopeType identifies scopes whose composition requires a transaction.
// Example: rows.TransactionInner[rows.TransactionAlias[a,User],rows.TransactionAlias[b,Order]].
type TransactionScopeType interface{ transactionScopeType() }

func (TransactionAlias[A, R]) transactionScopeType() {}

// TransactionJoinSource retains transaction scope and complete alias boundaries.
// Example: rows.TransactionLeftJoin(users,orders,on).
type TransactionJoinSource[S TransactionScopeType] interface{ TransactionSelectionSource[S] }

// TransactionJoinTable admits a single transaction alias on a join's right side.
// Example: rows.TransactionInnerJoin(users,orders,on).
type TransactionJoinTable[S TransactionScopeType] interface {
	TransactionJoinSource[S]
	transactionJoinTable(*S)
}

func (a TransactionAliased[A, R]) transactionJoinTable(*TransactionAlias[A, R]) {}

func buildTransactionJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R], on JoinOn[L, R], kind querycore.JoinKind, scope reflect.Type) joinState {
	l, r := transactionSelectionOf(left), transactionSelectionOf(right)
	return joinState{source: ormcore.JoinSources(l, r, on.core, kind, scope), left: l.Owners, right: r.Owners}
}

// TransactionInner identifies a transaction-only inner join scope.
// Example: rows.TransactionInner[L,R].
type TransactionInner[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionInnerPhase [0]struct{}
}

func (TransactionInner[L, R]) transactionScopeType() {}

// TransactionInnerJoined retains both input boundaries and their nested locks.
// Example: rows.TransactionInnerJoin(left,right,on).
type TransactionInnerJoined[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionInnerPhase [0]struct{}
	state                 joinState
}

func (j TransactionInnerJoined[L, R]) transactionSelection(*TransactionInner[L, R]) ormcore.RecordSource {
	return ormcore.RecordSource{Source: j.state.source, Transaction: true}
}
func (j TransactionInnerJoined[L, R]) transactionCorrelationScope(*TransactionInner[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(j.state.source)
}
func (j TransactionInnerJoined[L, R]) leftBoundary(*TransactionInner[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionInnerJoined[L, R]) preservedLeft(*TransactionInner[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionInnerJoined[L, R]) preservedRight(*TransactionInner[L, R], *R) scopeBoundary {
	return scopeBoundary{j.state.right, j.state.source.Err}
}

// TransactionInnerJoin preserves filters, windows and locks at each alias boundary.
// Example: rows.TransactionInnerJoin(left,right,on).
func TransactionInnerJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionInnerJoined[L, R] {
	return TransactionInnerJoined[L, R]{state: buildTransactionJoin(left, right, on, querycore.InnerJoin, reflect.TypeFor[TransactionInner[L, R]]())}
}

// TransactionLeft identifies a transaction-only left join scope.
// Example: rows.TransactionLeft[L,R].
type TransactionLeft[L, R TransactionScopeType] struct {
	_                    [0]*L
	_                    [0]*R
	transactionLeftPhase [0]struct{}
}

func (TransactionLeft[L, R]) transactionScopeType() {}

// TransactionLeftJoined retains both input boundaries and their nested locks.
// Example: rows.TransactionLeftJoin(left,right,on).
type TransactionLeftJoined[L, R TransactionScopeType] struct {
	_                    [0]*L
	_                    [0]*R
	transactionLeftPhase [0]struct{}
	state                joinState
}

func (j TransactionLeftJoined[L, R]) transactionSelection(*TransactionLeft[L, R]) ormcore.RecordSource {
	return ormcore.RecordSource{Source: j.state.source, Transaction: true}
}
func (j TransactionLeftJoined[L, R]) transactionCorrelationScope(*TransactionLeft[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(j.state.source)
}
func (j TransactionLeftJoined[L, R]) leftBoundary(*TransactionLeft[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionLeftJoined[L, R]) preservedLeft(*TransactionLeft[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionLeftJoined[L, R]) nullableRight(*TransactionLeft[L, R], *R) scopeBoundary {
	return scopeBoundary{j.state.right, j.state.source.Err}
}

// TransactionLeftJoin preserves filters, windows and locks at each alias boundary.
// Example: rows.TransactionLeftJoin(left,right,on).
func TransactionLeftJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionLeftJoined[L, R] {
	return TransactionLeftJoined[L, R]{state: buildTransactionJoin(left, right, on, querycore.LeftJoin, reflect.TypeFor[TransactionLeft[L, R]]())}
}

// TransactionRight identifies a transaction-only right join scope.
// Example: rows.TransactionRight[L,R].
type TransactionRight[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionRightPhase [0]struct{}
}

func (TransactionRight[L, R]) transactionScopeType() {}

// TransactionRightJoined retains both input boundaries and their nested locks.
// Example: rows.TransactionRightJoin(left,right,on).
type TransactionRightJoined[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionRightPhase [0]struct{}
	state                 joinState
}

func (j TransactionRightJoined[L, R]) transactionSelection(*TransactionRight[L, R]) ormcore.RecordSource {
	return ormcore.RecordSource{Source: j.state.source, Transaction: true}
}
func (j TransactionRightJoined[L, R]) transactionCorrelationScope(*TransactionRight[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(j.state.source)
}
func (j TransactionRightJoined[L, R]) leftBoundary(*TransactionRight[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionRightJoined[L, R]) nullableLeft(*TransactionRight[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionRightJoined[L, R]) preservedRight(*TransactionRight[L, R], *R) scopeBoundary {
	return scopeBoundary{j.state.right, j.state.source.Err}
}

// TransactionRightJoin preserves filters, windows and locks at each alias boundary.
// Example: rows.TransactionRightJoin(left,right,on).
func TransactionRightJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionRightJoined[L, R] {
	return TransactionRightJoined[L, R]{state: buildTransactionJoin(left, right, on, querycore.RightJoin, reflect.TypeFor[TransactionRight[L, R]]())}
}

// TransactionFull identifies a transaction-only full join scope.
// Example: rows.TransactionFull[L,R].
type TransactionFull[L, R TransactionScopeType] struct {
	_                    [0]*L
	_                    [0]*R
	transactionFullPhase [0]struct{}
}

func (TransactionFull[L, R]) transactionScopeType() {}

// TransactionFullJoined retains both input boundaries and their nested locks.
// Example: rows.TransactionFullJoin(left,right,on).
type TransactionFullJoined[L, R TransactionScopeType] struct {
	_                    [0]*L
	_                    [0]*R
	transactionFullPhase [0]struct{}
	state                joinState
}

func (j TransactionFullJoined[L, R]) transactionSelection(*TransactionFull[L, R]) ormcore.RecordSource {
	return ormcore.RecordSource{Source: j.state.source, Transaction: true}
}
func (j TransactionFullJoined[L, R]) transactionCorrelationScope(*TransactionFull[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(j.state.source)
}
func (j TransactionFullJoined[L, R]) leftBoundary(*TransactionFull[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionFullJoined[L, R]) nullableLeft(*TransactionFull[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionFullJoined[L, R]) nullableRight(*TransactionFull[L, R], *R) scopeBoundary {
	return scopeBoundary{j.state.right, j.state.source.Err}
}

// TransactionFullJoin preserves filters, windows and locks at each alias boundary.
// Example: rows.TransactionFullJoin(left,right,on).
func TransactionFullJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R], on JoinOn[L, R]) TransactionFullJoined[L, R] {
	return TransactionFullJoined[L, R]{state: buildTransactionJoin(left, right, on, querycore.FullJoin, reflect.TypeFor[TransactionFull[L, R]]())}
}

// TransactionCross identifies a transaction-only cross join scope.
// Example: rows.TransactionCross[L,R].
type TransactionCross[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionCrossPhase [0]struct{}
}

func (TransactionCross[L, R]) transactionScopeType() {}

// TransactionCrossJoined retains both input boundaries and their nested locks.
// Example: rows.TransactionCrossJoin(left,right).
type TransactionCrossJoined[L, R TransactionScopeType] struct {
	_                     [0]*L
	_                     [0]*R
	transactionCrossPhase [0]struct{}
	state                 joinState
}

func (j TransactionCrossJoined[L, R]) transactionSelection(*TransactionCross[L, R]) ormcore.RecordSource {
	return ormcore.RecordSource{Source: j.state.source, Transaction: true}
}
func (j TransactionCrossJoined[L, R]) transactionCorrelationScope(*TransactionCross[L, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(j.state.source)
}
func (j TransactionCrossJoined[L, R]) leftBoundary(*TransactionCross[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionCrossJoined[L, R]) preservedLeft(*TransactionCross[L, R], *L) scopeBoundary {
	return scopeBoundary{j.state.left, j.state.source.Err}
}
func (j TransactionCrossJoined[L, R]) preservedRight(*TransactionCross[L, R], *R) scopeBoundary {
	return scopeBoundary{j.state.right, j.state.source.Err}
}

// TransactionCrossJoin preserves filters, windows and locks at each alias boundary.
// Example: rows.TransactionCrossJoin(left,right).
func TransactionCrossJoin[L, R TransactionScopeType](left TransactionJoinSource[L], right TransactionJoinTable[R]) TransactionCrossJoined[L, R] {
	return TransactionCrossJoined[L, R]{state: buildTransactionJoin(left, right, JoinOn[L, R]{}, querycore.CrossJoin, reflect.TypeFor[TransactionCross[L, R]]())}
}
