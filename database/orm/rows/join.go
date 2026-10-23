// Package rows provides typed aliases, joins and complete row projections.
// Example: rows.Select(joined,OrderRows,rows.Map(OrderRows.ID,orderID)).
package rows

import (
	"errors"
	"maps"
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

// Source accepts only a source in S. Example: rows.Select(joined,OrderRows,mappings...).
type Source[S any] interface{ rowSource(*S) ormcore.Source }

// JoinTable accepts only a single aliased table on the right side of a join.
type JoinTable[S any] interface {
	Source[S]
	joinTable(*S)
}

func (a Aliased[A, M]) joinTable(*Alias[A, M]) {}

func (a Aliased[A, M]) rowSource(*Alias[A, M]) ormcore.Source     { return a.source }
func (j InnerJoined[L, R]) rowSource(*Inner[L, R]) ormcore.Source { return j.source }

// JoinOn retains the two operand scopes. Example: rows.On(memberID,orderMemberID).
type JoinOn[L, R any] struct {
	_           [0]*L
	_           [0]*R
	node        querycore.Expression
	left, right []ormcore.Reference
	err         error
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

// Scope selects the model scope of an alias. Example: expr.At(rows.Scope(members),Members.ID).
func Scope[A, M any](a Aliased[A, M]) expr.Scope[Alias[A, M], M] {
	return expr.FoundryScope[Alias[A, M], M](a.scope, sqlowner.Expressions())
}

// On joins expressions with the same value type. Example: rows.On(memberID,orderMemberID).
func On[L, R any, K comparable, LP, RP any](left expr.Expression[L, K, LP], right expr.Expression[R, K, RP]) JoinOn[L, R] {
	l, r := expr.FoundryExpression(left, sqlowner.Expressions()), expr.FoundryExpression(right, sqlowner.Expressions())
	return JoinOn[L, R]{node: querycore.BinaryComparison{Left: l.Node, Right: r.Node, Operator: querycore.Equal}, left: l.Refs, right: r.Refs, err: errors.Join(l.Err, r.Err)}
}

// InnerJoin joins two distinct typed aliases. Example: rows.InnerJoin(members,orders,rows.On(mID,oMemberID)).
func InnerJoin[L, R any](left Source[L], right JoinTable[R], on JoinOn[L, R]) InnerJoined[L, R] {
	if ormcore.NilDescriptor(left) || ormcore.NilDescriptor(right) {
		return InnerJoined[L, R]{source: ormcore.Source{Err: ormcore.Invalid("join requires both sources")}}
	}
	l, r := left.rowSource(nil), right.rowSource(nil)
	err := errors.Join(on.err, ormcore.CheckReferences(l, on.left), ormcore.CheckReferences(r, on.right))
	if len(r.Node.Joins) > 0 {
		err = errors.Join(err, ormcore.Invalid("right join input must be an aliased source"))
	}
	node := querycore.JoinNodes(l.Node, r.Node, l.Aliases, r.Aliases, errors.Join(l.Err, err), r.Err, on.node, querycore.InnerJoin)
	owners := maps.Clone(l.Owners)
	if owners == nil {
		owners = map[string]any{}
	}
	maps.Copy(owners, r.Owners)
	return InnerJoined[L, R]{source: ormcore.Source{Scope: reflect.TypeFor[Inner[L, R]](), Node: node.Node, Aliases: node.Aliases, Owners: owners, Err: node.Err, Checks: ormcore.AppendChecks(l.Checks, r.Checks...)}, left: l.Owners, right: r.Owners}
}

// LeftScope re-scopes only a model from the left input. Example: rows.LeftScope(joined,rows.Scope(members)).
func LeftScope[L, R, M any](j InnerJoined[L, R], scope expr.Scope[L, M]) expr.Scope[Inner[L, R], M] {
	return rescope[Inner[L, R]](scope, j.left, j.source.Err)
}

// RightScope re-scopes only a model from the right input. Example: rows.RightScope(joined,rows.Scope(orders)).
func RightScope[L, R, M any](j InnerJoined[L, R], scope expr.Scope[R, M]) expr.Scope[Inner[L, R], M] {
	return rescope[Inner[L, R]](scope, j.right, j.source.Err)
}
func rescope[S, Old, M any](scope expr.Scope[Old, M], owners map[string]any, err error) expr.Scope[S, M] {
	c := expr.FoundryScopeData(scope, sqlowner.Expressions())
	c.Err = errors.Join(c.Err, err)
	if c.Owner == nil || owners[c.Table] != c.Owner {
		c.Err = errors.Join(c.Err, ormcore.Invalid("scope is not on this join side"))
	}
	return expr.FoundryScope[S, M](c, sqlowner.Expressions())
}
