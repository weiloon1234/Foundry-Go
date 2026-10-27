package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CorrelatedRecord keeps a complete record bound to its enclosing scope. Example: rows.SelectCorrelatedRecord(c,rows.InnerScope(c,scope)).
type CorrelatedRecord[O, I, R any] struct {
	_    [0]*O
	_    [0]*I
	_    [0]*R
	core ormcore.CorrelatedRecord
}

// SelectCorrelatedRecord selects a complete preserved inner model. Example: rows.SelectCorrelatedRecord(c,rows.InnerScope(c,rows.Scope(child))).
func SelectCorrelatedRecord[O, I, R any](c CorrelatedSource[O, I], scope expr.Scope[Correlation[O, I], R]) CorrelatedRecord[O, I, R] {
	return CorrelatedRecord[O, I, R]{core: ormcore.SelectCorrelationRecord(c.core, expr.FoundryScopeData(scope, sqlowner.Expressions()))}
}

// ProjectCorrelated maps a complete declared record without granting execution. Example: rows.ProjectCorrelated(c,Totals,rows.MapSelected(Totals.Count,count)).
func ProjectCorrelated[O, I, R any](c CorrelatedSource[O, I], schema Schema[R], mappings ...Mapping[Correlation[O, I], R]) CorrelatedRecord[O, I, R] {
	return CorrelatedRecord[O, I, R]{core: projectCorrelation(c.core, schema, mappings)}
}

// Where appends lifted inner predicates. Example: record.Where(expr.Equal(childID,parentID)).
func (r CorrelatedRecord[O, I, R]) Where(ps ...expr.Predicate[Correlation[O, I]]) CorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = whereSource(r.core.Correlation.Source, ps)
	return r
}

// OrderBy appends inner result ordering. Example: record.OrderBy(childID.Desc()).
func (r CorrelatedRecord[O, I, R]) OrderBy(os ...expr.Order[Correlation[O, I]]) CorrelatedRecord[O, I, R] {
	r.core.Correlation.Source = orderSource(r.core.Correlation.Source, os)
	return r
}

// Limit retains the record's inner limit. Example: record.Limit(1).
func (r CorrelatedRecord[O, I, R]) Limit(n int) CorrelatedRecord[O, I, R] {
	r.core.Correlation.Source.Node.Limit = value.Set(n)
	return r
}

// Offset retains the record's inner offset. Example: record.Offset(1).
func (r CorrelatedRecord[O, I, R]) Offset(n int) CorrelatedRecord[O, I, R] {
	r.core.Correlation.Source.Node.Offset = n
	return r
}

// Exists tests the record within its required outer scope. Example: record.Exists().
func (r CorrelatedRecord[O, I, R]) Exists() expr.Predicate[O] {
	return expr.FoundryPredicateValue[O](ormcore.CorrelatedExists(r.core.Correlation), sqlowner.Expressions())
}

func projectCorrelation[S, R any](c ormcore.Correlation, schema Schema[R], mappings []Mapping[S, R]) ormcore.CorrelatedRecord {
	q := Select(selectionAdapter[S]{c.Source}, schema, mappings...)
	c.Source = q.source
	r := ormcore.CorrelatedRecord{Correlation: c}
	if !ormcore.NilDescriptor(schema) {
		core, _ := schema.rowDefinition(nil)
		if core != nil {
			r.Owner = core
			r.Columns = make([]querycore.Column, len(core.columns))
			for i, column := range core.columns {
				r.Columns[i] = querycore.Column{Name: column.Name, Nullable: column.Nullable}
			}
		}
	}
	if r.Owner == nil {
		r.Correlation.Source.Err = safeerror.Join("operation failed", r.Correlation.Source.Err, ormcore.Invalid("correlated projection requires a declaration"))
	}
	return r
}
