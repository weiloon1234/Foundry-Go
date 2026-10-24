package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TransactionCorrelation identifies an explicit enclosing/inner scope pair. Example: TransactionCorrelation[TransactionAlias[parent,User],TransactionAlias[child,Order]].
type TransactionCorrelation[O, I TransactionScopeType] struct {
	_                           [0]*O
	_                           [0]*I
	transactionCorrelationPhase [0]struct{}
}

// TransactionScopeSource exposes enclosing visibility without granting execution. Example: rows.TransactionCorrelate(parent,child).
type TransactionScopeSource[S TransactionScopeType] interface{ transactionCorrelationScope(*S) ormcore.VisibleScope }

// TransactionCorrelatedSource retains its enclosing scope and cannot execute independently. Example: rows.TransactionCorrelate(parent,child).
type TransactionCorrelatedSource[O, I TransactionScopeType] struct {
	_                           [0]*O
	_                           [0]*I
	transactionCorrelationPhase [0]struct{}
	core                        ormcore.Correlation
}

func (c TransactionCorrelatedSource[O, I]) transactionCorrelationScope(*TransactionCorrelation[O, I]) ormcore.VisibleScope {
	return ormcore.CorrelationVisibility(c.core)
}

// TransactionCorrelate makes only the supplied enclosing scopes visible inside inner. Example: rows.TransactionCorrelate(parent,child).
func TransactionCorrelate[O, I TransactionScopeType](outer TransactionScopeSource[O], inner TransactionSelectionSource[I]) TransactionCorrelatedSource[O, I] {
	var visible ormcore.VisibleScope
	if ormcore.NilDescriptor(outer) {
		visible.Required.Err = ormcore.Invalid("correlation requires an outer scope")
	} else {
		visible = outer.transactionCorrelationScope(nil)
	}
	return TransactionCorrelatedSource[O, I]{core: ormcore.BindCorrelation(visible, transactionSelectionOf(inner), reflect.TypeFor[O](), reflect.TypeFor[TransactionCorrelation[O, I]]())}
}

// TransactionOuterScope lifts a preserved enclosing model into a correlation. Example: rows.TransactionOuterScope(c,rows.TransactionScope(parent)).
func TransactionOuterScope[O, I TransactionScopeType, M any](c TransactionCorrelatedSource[O, I], scope expr.Scope[O, M]) expr.Scope[TransactionCorrelation[O, I], M] {
	return expr.FoundryScope[TransactionCorrelation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()), true), sqlowner.Expressions())
}

// TransactionInnerScope lifts a preserved inner model into a correlation. Example: rows.TransactionInnerScope(c,rows.TransactionScope(child)).
func TransactionInnerScope[O, I TransactionScopeType, M any](c TransactionCorrelatedSource[O, I], scope expr.Scope[I, M]) expr.Scope[TransactionCorrelation[O, I], M] {
	return expr.FoundryScope[TransactionCorrelation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()), false), sqlowner.Expressions())
}

// TransactionOuterNullableScope preserves an enclosing outer join's nullability. Example: rows.TransactionOuterNullableScope(c,rows.NullableRightScope(j,scope)).
func TransactionOuterNullableScope[O, I TransactionScopeType, M any](c TransactionCorrelatedSource[O, I], scope expr.NullableScope[O, M]) expr.NullableScope[TransactionCorrelation[O, I], M] {
	return expr.FoundryNullableScope[TransactionCorrelation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), true), sqlowner.Expressions())
}

// TransactionInnerNullableScope preserves an inner outer join's nullability. Example: rows.TransactionInnerNullableScope(c,rows.NullableRightScope(j,scope)).
func TransactionInnerNullableScope[O, I TransactionScopeType, M any](c TransactionCorrelatedSource[O, I], scope expr.NullableScope[I, M]) expr.NullableScope[TransactionCorrelation[O, I], M] {
	return expr.FoundryNullableScope[TransactionCorrelation[O, I], M](ormcore.CorrelationScope(c.core, expr.FoundryNullableScopeData(scope, sqlowner.Expressions()), false), sqlowner.Expressions())
}

// Where appends predicates in the lifted correlation scope. Example: c.Where(expr.Equal(childID,parentID)).
func (c TransactionCorrelatedSource[O, I]) Where(ps ...expr.Predicate[TransactionCorrelation[O, I]]) TransactionCorrelatedSource[O, I] {
	c.core.Source = whereSource(c.core.Source, ps)
	return c
}

// OrderBy appends scoped ordering. Example: c.OrderBy(childID.Desc()).
func (c TransactionCorrelatedSource[O, I]) OrderBy(os ...expr.Order[TransactionCorrelation[O, I]]) TransactionCorrelatedSource[O, I] {
	c.core.Source = orderSource(c.core.Source, os)
	return c
}

// Limit retains an inner result limit. Example: c.Limit(1).
func (c TransactionCorrelatedSource[O, I]) Limit(n int) TransactionCorrelatedSource[O, I] {
	c.core.Source.Node.Limit = value.Set(n)
	return c
}

// Offset retains an inner result offset. Example: c.Offset(1).
func (c TransactionCorrelatedSource[O, I]) Offset(n int) TransactionCorrelatedSource[O, I] {
	c.core.Source.Node.Offset = n
	return c
}

// Exists returns a predicate in the enclosing scope. Example: expr.Where(users,c.Exists()).
func (c TransactionCorrelatedSource[O, I]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(c.core), sqlowner.Expressions())
}

func (TransactionCorrelation[O, I]) transactionScopeType() {}
func (a TransactionAliased[A, R]) transactionCorrelationScope(*TransactionAlias[A, R]) ormcore.VisibleScope {
	return ormcore.SourceVisibility(a.core.Source)
}
