package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// LateralSource retains its required preceding scope without ordinary source methods. Example: rows.AsLateral[latest](record,"latest").
type LateralSource[A, O, R any] struct {
	_      [0]*A
	_      [0]*O
	_      [0]*R
	source ormcore.Source
	outer  ormcore.VisibleScope
}

// AsLateral always creates a derived SELECT for a correlated record. Example: rows.AsLateral[latest](record.Limit(1),"latest").
func AsLateral[A, O, I, R any](record CorrelatedRecord[O, I, R], alias string) LateralSource[A, O, R] {
	return LateralSource[A, O, R]{source: ormcore.AliasLateral(record.core, alias, reflect.TypeFor[Alias[A, R]]()), outer: record.core.Correlation.Outer}
}

// LateralScope exposes the named complete record for ON and result mapping. Example: expr.At(rows.LateralScope(latest), orm.KeyExpr(Orders.ID)).
func LateralScope[A, O, R any](source LateralSource[A, O, R]) expr.Scope[Alias[A, R], R] {
	return expr.FoundryScope[Alias[A, R], R](ormcore.Scope{Reference: ormcore.Reference{Table: source.source.Node.Source.Name(), Owner: source.source.Owners[source.source.Node.Source.Name()]}, Err: source.source.Err}, sqlowner.Expressions())
}

// CrossJoinLateral evaluates right for each preceding left row. Example: rows.CrossJoinLateral(parent,latest).
func CrossJoinLateral[L, A, R any](left Source[L], right LateralSource[A, L, R]) CrossJoined[L, Alias[A, R]] {
	s := buildLateral(left, right, nil, querycore.CrossJoin, reflect.TypeFor[Cross[L, Alias[A, R]]]())
	return CrossJoined[L, Alias[A, R]]{source: s.source, left: s.left, right: s.right}
}

// InnerJoinLateral combines optional typed ON conditions; no conditions means ON TRUE. Example: rows.InnerJoinLateral(parent,latest).
func InnerJoinLateral[L, A, R any](left Source[L], right LateralSource[A, L, R], on ...JoinOn[L, Alias[A, R]]) InnerJoined[L, Alias[A, R]] {
	s := buildLateral(left, right, on, querycore.InnerJoin, reflect.TypeFor[Inner[L, Alias[A, R]]]())
	return InnerJoined[L, Alias[A, R]]{source: s.source, left: s.left, right: s.right}
}

// LeftJoinLateral preserves unmatched parents and widens the right scope. Example: rows.LeftJoinLateral(parent,latest).
func LeftJoinLateral[L, A, R any](left Source[L], right LateralSource[A, L, R], on ...JoinOn[L, Alias[A, R]]) LeftJoined[L, Alias[A, R]] {
	s := buildLateral(left, right, on, querycore.LeftJoin, reflect.TypeFor[Left[L, Alias[A, R]]]())
	return LeftJoined[L, Alias[A, R]]{source: s.source, left: s.left, right: s.right}
}

func buildLateral[L, A, R any](left Source[L], right LateralSource[A, L, R], on []JoinOn[L, Alias[A, R]], kind querycore.JoinKind, scope reflect.Type) joinState {
	if ormcore.NilDescriptor(left) {
		return joinState{source: ormcore.Source{Err: ormcore.Invalid("lateral requires a preceding source")}}
	}
	l := left.rowSource(nil)
	conditions := make([]ormcore.JoinCondition, len(on))
	for i, c := range on {
		conditions[i] = c.core
	}
	return joinState{source: ormcore.JoinLateral(l, right.source, right.outer, conditions, kind, scope), left: l.Owners, right: right.source.Owners}
}
