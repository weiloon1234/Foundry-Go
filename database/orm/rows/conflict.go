package rows

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ConflictRow separates stored/proposed expressions from ordinary model queries. Example: rows.ConflictRow[User].
type ConflictRow[M any] struct {
	_             [0]*M
	conflictPhase [0]struct{}
}

// ConflictRows exposes complete stored/proposed records without standalone SELECT. Example: rows.ConflictRowsOf(Users.Query()).
type ConflictRows[M any] struct {
	_    [0]*M
	core ormcore.VisibleScope
}

// ConflictRowsOf obtains declaration metadata; query filters do not become update conditions. Example: rows.ConflictRowsOf(Users.Query()).
func ConflictRowsOf[M any](q orm.Query[M]) ConflictRows[M] {
	return ConflictRows[M]{core: ormcore.ConflictVisibility(orm.FoundrySource(q, sqlowner.Metadata()), reflect.TypeFor[ConflictRow[M]]())}
}

// Stored identifies the existing conflicting row. Example: expr.At(records.Stored(), orm.NumberExpr(Users.Age)).
func (r ConflictRows[M]) Stored() expr.Scope[ConflictRow[M], M] {
	return expr.FoundryScope[ConflictRow[M], M](ormcore.ConflictScope(r.core, querycore.ConflictStoredTable), sqlowner.Expressions())
}

// Proposed identifies EXCLUDED, including defaults and trigger changes. Example: expr.At(records.Proposed(), orm.NumberExpr(Users.Age)).
func (r ConflictRows[M]) Proposed() expr.Scope[ConflictRow[M], M] {
	return expr.FoundryScope[ConflictRow[M], M](ormcore.ConflictScope(r.core, querycore.ConflictProposedTable), sqlowner.Expressions())
}

func (r ConflictRows[M]) correlationScope(*ConflictRow[M]) ormcore.VisibleScope { return r.core }

// OnConflictKeys infers a unique index using row-phase expressions. Example: rows.OnConflictKeys(expr.Group(expr.Add(age,age))).
func OnConflictKeys[M any](keys ...expr.GroupKey[M]) orm.Conflict[M] {
	inputs := make([]ormcore.Expression, len(keys))
	for i, key := range keys {
		inputs[i] = expr.FoundryGroup(key, sqlowner.Expressions())
	}
	return orm.FoundryConflict[M](ormcore.ConflictKeys(reflect.TypeFor[M](), inputs), sqlowner.Expressions())
}

// ConflictTargetWhere declares a partial index predicate using fixed migration values. Example: rows.ConflictTargetWhere(policy,expr.Eq(expr.Value(orm.ColumnExpr(Users.Active)), true)).
func ConflictTargetWhere[M any](policy orm.Conflict[M], predicates ...expr.Predicate[M]) orm.Conflict[M] {
	return orm.FoundryConflict[M](ormcore.ConflictPredicates(orm.FoundryConflictData(policy, sqlowner.Expressions()), reflect.TypeFor[M](), conflictPredicates(predicates), false), sqlowner.Expressions())
}

// ConflictWhereRows adds update conditions over stored and proposed records. Example: rows.ConflictWhereRows(policy,expr.Greater(proposed,stored)).
func ConflictWhereRows[M any](policy orm.Conflict[M], predicates ...expr.Predicate[ConflictRow[M]]) orm.Conflict[M] {
	return orm.FoundryConflict[M](ormcore.ConflictPredicates(orm.FoundryConflictData(policy, sqlowner.Expressions()), reflect.TypeFor[ConflictRow[M]](), conflictPredicates(predicates), true), sqlowner.Expressions())
}

func conflictPredicates[S any](predicates []expr.Predicate[S]) []ormcore.Predicate {
	out := make([]ormcore.Predicate, len(predicates))
	for i, p := range predicates {
		out[i] = expr.FoundryPredicate(p, sqlowner.Expressions())
	}
	return out
}

// SetConflictValue assigns an exact stored type, including nullability, from the conflict row phase. Example: rows.SetConflictValue(orm.NumberExpr(Users.Age), expr.Add(stored,proposed)).
func SetConflictValue[M, V, P any](field orm.ExpressionColumn[M, V, P], calculation expr.RowValue[ConflictRow[M], P]) orm.ConflictUpdate[M] {
	column, _ := orm.FoundryColumn(field, sqlowner.Metadata())
	input := expr.FoundryRowValue(calculation, sqlowner.Expressions())
	return orm.FoundryConflictUpdate[M](ormcore.ConflictCalculation(column.Reference, column.Field, column.Err, reflect.TypeFor[ConflictRow[M]](), input), sqlowner.Expressions())
}

// ConflictScalarRowQuery evaluates an independent scalar SELECT; no rows become NULL. Example: rows.ConflictScalarRowQuery(records,lookup).
func ConflictScalarRowQuery[M, I, V any](records ConflictRows[M], inner Scalar[I, V]) expr.RowExpression[ConflictRow[M], value.Nullable[V]] {
	core := ormcore.ScalarExpression(ormcore.ConflictAnchor(records.core), inner.source)
	return expr.NullableRow(expr.FoundryRowExpression[ConflictRow[M]](core, inner.column, sqlowner.Expressions()))
}

// ConflictScalarNullableRowQuery preserves one nullable layer for an independent scalar. Example: rows.ConflictScalarNullableRowQuery(records,lookup).
func ConflictScalarNullableRowQuery[M, I, V any](records ConflictRows[M], inner Scalar[I, value.Nullable[V]]) expr.RowExpression[ConflictRow[M], value.Nullable[V]] {
	return expr.FoundryRowExpression[ConflictRow[M]](ormcore.ScalarExpression(ormcore.ConflictAnchor(records.core), inner.source), inner.column, sqlowner.Expressions())
}
