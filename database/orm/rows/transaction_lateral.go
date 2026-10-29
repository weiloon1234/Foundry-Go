package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// TransactionLateralSource retains its required preceding scope without ordinary source methods. Example: rows.AsTransactionLateral[latest](record,"latest").
type TransactionLateralSource[A any, O TransactionScopeType, R any] struct {
	_                       [0]*A
	_                       [0]*O
	_                       [0]*R
	transactionLateralPhase [0]struct{}
	source                  ormcore.Source
	outer                   ormcore.VisibleScope
}

// AsTransactionLateral always creates a derived SELECT for a correlated record. Example: rows.AsTransactionLateral[latest](record.Limit(1),"latest").
func AsTransactionLateral[A any, O, I TransactionScopeType, R any](record TransactionCorrelatedRecord[O, I, R], alias string) TransactionLateralSource[A, O, R] {
	return TransactionLateralSource[A, O, R]{source: ormcore.AliasLateral(record.core, alias, reflect.TypeFor[TransactionAlias[A, R]]()), outer: record.core.Correlation.Outer}
}

// TransactionLateralScope exposes the named complete record for ON and result mapping. Example: expr.At(rows.TransactionLateralScope(latest), orm.KeyExpr(Orders.ID)).
func TransactionLateralScope[A any, O TransactionScopeType, R any](source TransactionLateralSource[A, O, R]) expr.Scope[TransactionAlias[A, R], R] {
	return expr.FoundryScope[TransactionAlias[A, R], R](ormcore.Scope{Reference: ormcore.Reference{Table: source.source.Node.Source.Name(), Owner: source.source.Owners[source.source.Node.Source.Name()]}, Err: source.source.Err}, sqlowner.Expressions())
}

// TransactionCrossJoinLateral evaluates right for each preceding left row. Example: rows.TransactionCrossJoinLateral(parent,latest).
func TransactionCrossJoinLateral[L TransactionScopeType, A, R any](left TransactionJoinSource[L], right TransactionLateralSource[A, L, R]) TransactionCrossJoined[L, TransactionAlias[A, R]] {
	s := buildTransactionLateral(left, right, nil, querycore.CrossJoin, reflect.TypeFor[TransactionCross[L, TransactionAlias[A, R]]]())
	return TransactionCrossJoined[L, TransactionAlias[A, R]]{state: s}
}

// TransactionInnerJoinLateral combines optional typed ON conditions; no conditions means ON TRUE. Example: rows.TransactionInnerJoinLateral(parent,latest).
func TransactionInnerJoinLateral[L TransactionScopeType, A, R any](left TransactionJoinSource[L], right TransactionLateralSource[A, L, R], on ...JoinOn[L, TransactionAlias[A, R]]) TransactionInnerJoined[L, TransactionAlias[A, R]] {
	s := buildTransactionLateral(left, right, on, querycore.InnerJoin, reflect.TypeFor[TransactionInner[L, TransactionAlias[A, R]]]())
	return TransactionInnerJoined[L, TransactionAlias[A, R]]{state: s}
}

// TransactionLeftJoinLateral preserves unmatched parents and widens the right scope. Example: rows.TransactionLeftJoinLateral(parent,latest).
func TransactionLeftJoinLateral[L TransactionScopeType, A, R any](left TransactionJoinSource[L], right TransactionLateralSource[A, L, R], on ...JoinOn[L, TransactionAlias[A, R]]) TransactionLeftJoined[L, TransactionAlias[A, R]] {
	s := buildTransactionLateral(left, right, on, querycore.LeftJoin, reflect.TypeFor[TransactionLeft[L, TransactionAlias[A, R]]]())
	return TransactionLeftJoined[L, TransactionAlias[A, R]]{state: s}
}

func buildTransactionLateral[L TransactionScopeType, A, R any](left TransactionJoinSource[L], right TransactionLateralSource[A, L, R], on []JoinOn[L, TransactionAlias[A, R]], kind querycore.JoinKind, scope reflect.Type) joinState {
	if ormcore.NilDescriptor(left) {
		return joinState{source: ormcore.Source{Err: ormcore.Invalid("lateral requires a preceding source")}}
	}
	l := transactionSelectionOf(left)
	conditions := make([]ormcore.JoinCondition, len(on))
	for i, c := range on {
		conditions[i] = c.core
	}
	return joinState{source: ormcore.JoinLateral(l, right.source, right.outer, conditions, kind, scope), left: l.Owners, right: right.source.Owners}
}
