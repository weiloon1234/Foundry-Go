package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Correlation identifies an explicit enclosing/inner scope pair. Example: Correlation[User,Order].
type Correlation[O, I any] struct {
	_                [0]*O
	_                [0]*I
	correlationPhase [0]struct{}
}

// ScopeSource exposes enclosing visibility without granting execution. Example: rows.Correlate(parent,child).
type ScopeSource[S any] interface{ correlationScope(*S) ormcore.VisibleScope }

// CorrelatedSource retains its enclosing scope and cannot execute independently. Example: rows.Correlate(rows.From(users),rows.From(orders)).
type CorrelatedSource[O, I any] struct {
	_    [0]*O
	_    [0]*I
	core ormcore.Correlation
}

func (c CorrelatedSource[O, I]) correlationScope(*Correlation[O, I]) ormcore.VisibleScope {
	return ormcore.CorrelationVisibility(c.core)
}

// Correlate makes only the supplied enclosing scopes visible inside inner. Example: rows.Correlate(parent,child).
func Correlate[O, I any](outer ScopeSource[O], inner SelectionSource[I]) CorrelatedSource[O, I] {
	var visible ormcore.VisibleScope
	if ormcore.NilDescriptor(outer) {
		visible.Required.Err = ormcore.Invalid("correlation requires an outer scope")
	} else {
		visible = outer.correlationScope(nil)
	}
	return CorrelatedSource[O, I]{core: ormcore.BindCorrelation(visible, sourceOf(inner), reflect.TypeFor[O](), reflect.TypeFor[Correlation[O, I]]())}
}

// ModelScope exposes an ordinary model query's declaration. Example: rows.ModelScope(Users.Query()).
func ModelScope[M any](q orm.Query[M]) expr.Scope[M, M] {
	s := orm.FoundrySource(q, sqlowner.Metadata())
	return expr.FoundryScope[M, M](ormcore.Scope{Reference: ormcore.Reference{Scope: s.Scope, Table: s.Node.Source.Name(), Owner: s.Owners[s.Node.Source.Name()]}, Err: s.Err}, sqlowner.Expressions())
}

// OuterScope lifts a preserved enclosing model into a correlation. Example: rows.OuterScope(c,rows.Scope(parent)).
func OuterScope[O, I, M any](c CorrelatedSource[O, I], scope expr.Scope[O, M]) expr.Scope[Correlation[O, I], M] {
	return expr.FoundryScope[Correlation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()), true), sqlowner.Expressions())
}

// InnerScope lifts a preserved inner model into a correlation. Example: rows.InnerScope(c,rows.Scope(child)).
func InnerScope[O, I, M any](c CorrelatedSource[O, I], scope expr.Scope[I, M]) expr.Scope[Correlation[O, I], M] {
	return expr.FoundryScope[Correlation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()), false), sqlowner.Expressions())
}

// OuterNullableScope preserves an enclosing outer join's nullability. Example: rows.OuterNullableScope(c,rows.NullableRightScope(j,scope)).
func OuterNullableScope[O, I, M any](c CorrelatedSource[O, I], scope expr.NullableScope[O, M]) expr.NullableScope[Correlation[O, I], M] {
	return expr.FoundryNullableScope[Correlation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), true), sqlowner.Expressions())
}

// InnerNullableScope preserves an inner outer join's nullability. Example: rows.InnerNullableScope(c,rows.NullableRightScope(j,scope)).
func InnerNullableScope[O, I, M any](c CorrelatedSource[O, I], scope expr.NullableScope[I, M]) expr.NullableScope[Correlation[O, I], M] {
	return expr.FoundryNullableScope[Correlation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), false), sqlowner.Expressions())
}

// Where appends predicates in the lifted correlation scope. Example: c.Where(expr.Equal(childID,parentID)).
func (c CorrelatedSource[O, I]) Where(ps ...expr.Predicate[Correlation[O, I]]) CorrelatedSource[O, I] {
	c.core.Source = whereSource(c.core.Source, ps)
	return c
}

// OrderBy appends scoped ordering. Example: c.OrderBy(childID.Desc()).
func (c CorrelatedSource[O, I]) OrderBy(os ...expr.Order[Correlation[O, I]]) CorrelatedSource[O, I] {
	c.core.Source = orderSource(c.core.Source, os)
	return c
}

// Limit retains an inner result limit. Example: c.Limit(1).
func (c CorrelatedSource[O, I]) Limit(n int) CorrelatedSource[O, I] {
	c.core.Source.Node.Limit = value.Set(n)
	return c
}

// Offset retains an inner result offset. Example: c.Offset(1).
func (c CorrelatedSource[O, I]) Offset(n int) CorrelatedSource[O, I] {
	c.core.Source.Node.Offset = n
	return c
}

// Exists returns a predicate in the enclosing scope. Example: expr.Where(users,c.Exists()).
func (c CorrelatedSource[O, I]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(c.core), sqlowner.Expressions())
}

func orderSource[S any](source ormcore.Source, orders []expr.Order[S]) ormcore.Source {
	values := make([]ormcore.Order, len(orders))
	for i, o := range orders {
		values[i] = expr.FoundryOrder(o, sqlowner.Expressions())
	}
	return ormcore.OrderSource(source, values)
}
