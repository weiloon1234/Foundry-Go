package rows

import (
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"reflect"
)

type joinState struct {
	source      ormcore.Source
	left, right map[string]any
}

func buildJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R], kind querycore.JoinKind, scope reflect.Type) joinState {
	if ormcore.NilDescriptor(left) || ormcore.NilDescriptor(right) {
		return joinState{source: ormcore.Source{Err: ormcore.Invalid("join requires both sources")}}
	}
	l, r := left.rowSource(nil), right.rowSource(nil)
	return joinState{source: ormcore.JoinSources(l, r, on.node, on.left, on.right, on.err, kind, scope), left: l.Owners, right: r.Owners}
}

type scopeBoundary struct {
	owners map[string]any
	err    error
}
type preservedLeftSource[S, L any] interface{ preservedLeft(*S, *L) scopeBoundary }
type preservedRightSource[S, R any] interface{ preservedRight(*S, *R) scopeBoundary }
type nullableLeftSource[S, L any] interface{ nullableLeft(*S, *L) scopeBoundary }
type nullableRightSource[S, R any] interface{ nullableRight(*S, *R) scopeBoundary }
type leftScopeSource[S, L any] interface{ leftBoundary(*S, *L) scopeBoundary }

func (j InnerJoined[L, R]) preservedLeft(*Inner[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j InnerJoined[L, R]) preservedRight(*Inner[L, R], *R) scopeBoundary {
	return scopeBoundary{j.right, j.source.Err}
}
func (j InnerJoined[L, R]) leftBoundary(*Inner[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}

// NullableLeftScope widens the missing left rows of a right/full join. Example: rows.NullableLeftScope(joined,rows.Scope(members)).
func NullableLeftScope[S, L, M any](j nullableLeftSource[S, L], scope expr.Scope[L, M]) expr.NullableScope[S, M] {
	b := scopeBoundary{err: ormcore.Invalid("scope requires a join")}
	if !ormcore.NilDescriptor(j) {
		b = j.nullableLeft(nil, nil)
	}
	return expr.FoundryNullableScope[S, M](ormcore.Rescope(expr.FoundryScopeData(scope, sqlowner.Expressions()), b.owners, b.err), sqlowner.Expressions())
}

// NullableRightScope widens the missing right rows of a left/full join. Example: rows.NullableRightScope(joined,rows.Scope(orders)).
func NullableRightScope[S, R, M any](j nullableRightSource[S, R], scope expr.Scope[R, M]) expr.NullableScope[S, M] {
	b := scopeBoundary{err: ormcore.Invalid("scope requires a join")}
	if !ormcore.NilDescriptor(j) {
		b = j.nullableRight(nil, nil)
	}
	return expr.FoundryNullableScope[S, M](ormcore.Rescope(expr.FoundryScopeData(scope, sqlowner.Expressions()), b.owners, b.err), sqlowner.Expressions())
}

// LeftNullableScope retains an already nullable scope through another join. Example: rows.LeftNullableScope(chain,nullableOrders).
func LeftNullableScope[S, L, M any](j leftScopeSource[S, L], scope expr.NullableScope[L, M]) expr.NullableScope[S, M] {
	b := scopeBoundary{err: ormcore.Invalid("scope requires a join")}
	if !ormcore.NilDescriptor(j) {
		b = j.leftBoundary(nil, nil)
	}
	return expr.FoundryNullableScope[S, M](ormcore.Rescope(expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), b.owners, b.err), sqlowner.Expressions())
}

// Left identifies a left join's distinct result scope. Example: rows.Left[L,R].
type Left[L, R any] struct {
	_         [0]*L
	_         [0]*R
	leftPhase [0]struct{}
}

// LeftJoined retains typed sides and non-generic join state. Example: rows.LeftJoin(a,b,on).
type LeftJoined[L, R any] struct {
	_           [0]*L
	_           [0]*R
	leftPhase   [0]struct{}
	source      ormcore.Source
	left, right map[string]any
}

func (j LeftJoined[L, R]) rowSource(*Left[L, R]) ormcore.Source { return j.source }
func (j LeftJoined[L, R]) leftBoundary(*Left[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j LeftJoined[L, R]) preservedLeft(*Left[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j LeftJoined[L, R]) nullableRight(*Left[L, R], *R) scopeBoundary {
	return scopeBoundary{j.right, j.source.Err}
}

// LeftJoin combines aliased inputs with left join semantics. Example: rows.LeftJoin(a,b,on).
func LeftJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R]) LeftJoined[L, R] {
	state := buildJoin(left, right, on, querycore.LeftJoin, reflect.TypeFor[Left[L, R]]())
	return LeftJoined[L, R]{source: state.source, left: state.left, right: state.right}
}

// Right identifies a right join's distinct result scope. Example: rows.Right[L,R].
type Right[L, R any] struct {
	_          [0]*L
	_          [0]*R
	rightPhase [0]struct{}
}

// RightJoined retains typed sides and non-generic join state. Example: rows.RightJoin(a,b,on).
type RightJoined[L, R any] struct {
	_           [0]*L
	_           [0]*R
	rightPhase  [0]struct{}
	source      ormcore.Source
	left, right map[string]any
}

func (j RightJoined[L, R]) rowSource(*Right[L, R]) ormcore.Source { return j.source }
func (j RightJoined[L, R]) leftBoundary(*Right[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j RightJoined[L, R]) nullableLeft(*Right[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j RightJoined[L, R]) preservedRight(*Right[L, R], *R) scopeBoundary {
	return scopeBoundary{j.right, j.source.Err}
}

// RightJoin combines aliased inputs with right join semantics. Example: rows.RightJoin(a,b,on).
func RightJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R]) RightJoined[L, R] {
	state := buildJoin(left, right, on, querycore.RightJoin, reflect.TypeFor[Right[L, R]]())
	return RightJoined[L, R]{source: state.source, left: state.left, right: state.right}
}

// Full identifies a full join's distinct result scope. Example: rows.Full[L,R].
type Full[L, R any] struct {
	_         [0]*L
	_         [0]*R
	fullPhase [0]struct{}
}

// FullJoined retains typed sides and non-generic join state. Example: rows.FullJoin(a,b,on).
type FullJoined[L, R any] struct {
	_           [0]*L
	_           [0]*R
	fullPhase   [0]struct{}
	source      ormcore.Source
	left, right map[string]any
}

func (j FullJoined[L, R]) rowSource(*Full[L, R]) ormcore.Source { return j.source }
func (j FullJoined[L, R]) leftBoundary(*Full[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j FullJoined[L, R]) nullableLeft(*Full[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j FullJoined[L, R]) nullableRight(*Full[L, R], *R) scopeBoundary {
	return scopeBoundary{j.right, j.source.Err}
}

// FullJoin combines aliased inputs with full join semantics. Example: rows.FullJoin(a,b,on).
func FullJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R]) FullJoined[L, R] {
	state := buildJoin(left, right, on, querycore.FullJoin, reflect.TypeFor[Full[L, R]]())
	return FullJoined[L, R]{source: state.source, left: state.left, right: state.right}
}

// Cross identifies a cross join's distinct result scope. Example: rows.Cross[L,R].
type Cross[L, R any] struct {
	_          [0]*L
	_          [0]*R
	crossPhase [0]struct{}
}

// CrossJoined retains typed sides and non-generic join state. Example: rows.CrossJoin(a,b).
type CrossJoined[L, R any] struct {
	_           [0]*L
	_           [0]*R
	crossPhase  [0]struct{}
	source      ormcore.Source
	left, right map[string]any
}

func (j CrossJoined[L, R]) rowSource(*Cross[L, R]) ormcore.Source { return j.source }
func (j CrossJoined[L, R]) leftBoundary(*Cross[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j CrossJoined[L, R]) preservedLeft(*Cross[L, R], *L) scopeBoundary {
	return scopeBoundary{j.left, j.source.Err}
}
func (j CrossJoined[L, R]) preservedRight(*Cross[L, R], *R) scopeBoundary {
	return scopeBoundary{j.right, j.source.Err}
}

// CrossJoin combines aliased inputs with cross join semantics. Example: rows.CrossJoin(a,b).
func CrossJoin[L, R any](left Source[L], right JoinTable[R]) CrossJoined[L, R] {
	state := buildJoin(left, right, JoinOn[L, R]{}, querycore.CrossJoin, reflect.TypeFor[Cross[L, R]]())
	return CrossJoined[L, R]{source: state.source, left: state.left, right: state.right}
}
