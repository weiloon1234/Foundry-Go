// Package rows provides typed aliases, joins and complete row projections.
// Example: rows.Select(joined,OrderRows,rows.Map(OrderRows.ID,orderID)).
package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Alias gives each model occurrence a distinct scope. Example: Alias[memberSide,Member].
type Alias[A, M any] struct {
	_ [0]*A
	_ [0]*M
}

// Inner preserves the scopes of both joined inputs. Example: Inner[Alias[left,Member],Alias[right,Order]].
type Inner[L, R any] struct {
	_ [0]*L
	_ [0]*R
}

// Aliased is a model source with one typed alias. Example: rows.As[memberSide](Members.Query(),"members").
type Aliased[A, M any] struct {
	_      [0]*A
	_      [0]*M
	source ormcore.Source
	scope  ormcore.Scope
}

// InnerJoined retains both sides for type-safe re-scoping. Example: rows.InnerJoin(members,orders,on).
type InnerJoined[L, R any] struct {
	_           [0]*L
	_           [0]*R
	source      ormcore.Source
	left, right map[string]any
}

// Source accepts an alias or join chain in S with a preserved join boundary.
// Example: rows.LeftJoin(users, orders, on).
type Source[S any] interface {
	SelectionSource[S]
	joinSource()
}

// JoinTable accepts only a single aliased table on the right side of a join.
type JoinTable[S any] interface {
	Source[S]
	joinTable(*S)
}

func (a Aliased[A, M]) joinSource()     {}
func (j InnerJoined[L, R]) joinSource() {}

func (a Aliased[A, M]) joinTable(*Alias[A, M]) {}

func (a Aliased[A, M]) rowSource(*Alias[A, M]) ormcore.Source     { return a.source }
func (j InnerJoined[L, R]) rowSource(*Inner[L, R]) ormcore.Source { return j.source }

// JoinOn retains the two operand scopes. Example: rows.On(memberID,orderMemberID).
type JoinOn[L, R any] struct {
	_    [0]*L
	_    [0]*R
	core ormcore.JoinCondition
}

// As aliases a model query, preserving its filters and soft-delete scope.
// Example: members := rows.As[memberSide](Members.Query(),"members").
func As[A, M any](q orm.Query[M], alias string) Aliased[A, M] {
	s := orm.FoundrySource(q, sqlowner.Metadata())
	direct := len(s.Node.Predicates) == 0 && len(s.Node.Orders) == 0 && !s.Node.Limit.IsSet() && s.Node.Offset == 0
	node := querycore.AliasNode(s.Node, s.Node.Source.Columns, direct, alias, reflect.TypeFor[Alias[A, M]](), s.Err)
	owner := s.Owners[s.Node.Source.Name()]
	return Aliased[A, M]{source: ormcore.Source{Scope: reflect.TypeFor[Alias[A, M]](), Node: node.Node, Aliases: node.Aliases, Owners: map[string]any{alias: owner}, Err: node.Err, Checks: s.Checks}, scope: ormcore.Scope{Reference: ormcore.Reference{Table: alias, Owner: owner}, Err: node.Err}}
}

// Scope selects the model scope of an alias. Example: expr.At(rows.Scope(members),orm.KeyExpr(Members.ID)).
func Scope[A, M any](a Aliased[A, M]) expr.Scope[Alias[A, M], M] {
	return expr.FoundryScope[Alias[A, M], M](a.scope, sqlowner.Expressions())
}

// On joins expressions with the same value type. Example: rows.On(memberID,orderMemberID).
func On[L, R any, K comparable, LP, RP any](left expr.Expression[L, K, LP], right expr.Expression[R, K, RP]) JoinOn[L, R] {
	l, r := expr.FoundryExpression(left, sqlowner.Expressions()), expr.FoundryExpression(right, sqlowner.Expressions())
	return JoinOn[L, R]{core: ormcore.JoinComparison(l, r, querycore.Equal)}
}

// InnerJoin joins two distinct typed aliases. Example: rows.InnerJoin(members,orders,rows.On(mID,oMemberID)).
func InnerJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R]) InnerJoined[L, R] {
	state := buildJoin(left, right, on, querycore.InnerJoin, reflect.TypeFor[Inner[L, R]]())
	return InnerJoined[L, R]{source: state.source, left: state.left, right: state.right}
}

// LeftScope preserves a model from the non-nullable left side. Example: rows.LeftScope(joined,rows.Scope(members)).
func LeftScope[S, L, M any](j preservedLeftSource[S, L], scope expr.Scope[L, M]) expr.Scope[S, M] {
	b := scopeBoundary{err: ormcore.Invalid("scope requires a join")}
	if !ormcore.NilDescriptor(j) {
		b = j.preservedLeft(nil, nil)
	}
	return rescope[S](scope, b.owners, b.err)
}

// RightScope preserves a model from the non-nullable right side. Example: rows.RightScope(joined,rows.Scope(orders)).
func RightScope[S, R, M any](j preservedRightSource[S, R], scope expr.Scope[R, M]) expr.Scope[S, M] {
	b := scopeBoundary{err: ormcore.Invalid("scope requires a join")}
	if !ormcore.NilDescriptor(j) {
		b = j.preservedRight(nil, nil)
	}
	return rescope[S](scope, b.owners, b.err)
}

func rescope[S, Old, M any](scope expr.Scope[Old, M], owners map[string]any, err error) expr.Scope[S, M] {
	c := expr.FoundryScopeData(scope, sqlowner.Expressions())
	return expr.FoundryScope[S, M](ormcore.Rescope(c, owners, err), sqlowner.Expressions())
}
