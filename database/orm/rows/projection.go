package rows

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

// Mapping binds a row field to a same-typed expression in S. Example: rows.Map(OrderRows.Label,label).
type Mapping[S, R any] struct {
	_          [0]*S
	_          [0]*R
	field      *fieldCore
	expression ormcore.Expression
}

// Query retains input scope S and result R without heavy engine types. Example: rows.Read(projection).All(ctx,db).
type Query[S, R any] struct {
	_           [0]*S
	source      ormcore.Source
	decode      func(database.Row) (R, error)
	recordOwner any
}

// Map requires an exact result-field value type. Example: rows.Map(OrderRows.Amount,amount).
func Map[S, R, V, P any](field Field[R, P], expression expr.Expression[S, V, P]) Mapping[S, R] {
	return Mapping[S, R]{field: field.core, expression: expr.FoundryExpression(expression, sqlowner.Expressions())}
}

// MapSelected binds a row field to an exactly typed selected value, including aggregates.
// Example: rows.MapSelected(Totals.Count,expr.AsSelected(expr.Count[Scope]())).
func MapSelected[S, R, V any](field Field[R, V], expression expr.Selected[S, V]) Mapping[S, R] {
	return Mapping[S, R]{field: field.core, expression: expr.FoundrySelected(expression, sqlowner.Expressions())}
}

// Select maps every declared row field exactly once. Example: rows.Select(joined,OrderRows,rows.Map(OrderRows.ID,id),...).
func Select[S, R any](source SelectionSource[S], schema Schema[R], mappings ...Mapping[S, R]) Query[S, R] {
	q := Query[S, R]{}
	if ormcore.NilDescriptor(source) || ormcore.NilDescriptor(schema) {
		q.source.Err = ormcore.Invalid("projection requires a source and row declaration")
		return q
	}
	q.source = source.rowSource(nil)
	core, decode := schema.rowDefinition(nil)
	q.decode = decode
	q.recordOwner = core
	if core == nil {
		q.source.Err = safeerror.Join("operation failed", q.source.Err, ormcore.Invalid("uninitialized row declaration"))
		return q
	}
	q.source.Err = safeerror.Join("operation failed", q.source.Err, core.err)
	if core.err == nil {
		q.source.Checks = ormcore.AppendChecks(q.source.Checks, core.check)
	}
	lowered := make([]querycore.ProjectionMapping, len(mappings))
	for i, m := range mappings {
		if m.field == nil || m.field.owner != core {
			q.source.Err = safeerror.Join("operation failed", q.source.Err, ormcore.Invalid("projection field belongs to a different row declaration"))
			continue
		}
		q.source.Err = safeerror.Join("operation failed", q.source.Err, m.expression.Err, ormcore.CheckReferenceSet(q.source, m.expression.Refs, m.expression.ReferenceFree))
		lowered[i] = querycore.ProjectionMapping{Column: m.field.column, Expression: m.expression.Node, Sensitive: m.expression.Sensitive}
	}
	node, err := querycore.SelectProjection(q.source.Node, core.columns, lowered)
	q.source.Node = node
	q.source.Err = safeerror.Join("operation failed", q.source.Err, err)
	return q
}

// SelectValue selects just a column's value type. Example: rows.SelectValue(Orders.Query(), orm.NumberExpr(Orders.TotalCents)).
func SelectValue[M, V, P any](source orm.Query[M], column orm.ExpressionColumn[M, V, P]) Query[M, P] {
	s := orm.FoundrySource(source, sqlowner.Metadata())
	_, c := orm.FoundryColumn(column, sqlowner.Metadata())
	s.Err = safeerror.Join("operation failed", s.Err, c.Err, ormcore.CheckReferences(s, []ormcore.Reference{c.Reference}))
	s.Node.Selections = []querycore.SelectItem{{Expression: c.Field, Alias: "value", Sensitive: c.Sensitive}}
	return Query[M, P]{source: s, decode: func(row database.Row) (P, error) {
		var result P
		if c.Scan == nil {
			return result, ormcore.Invalid("missing column decoder")
		}
		err := row.Scan(c.Scan(&result))
		return result, err
	}}
}

// Where appends scoped predicates. Example: report.Where(expr.Eq(memberTenant, tenant)).
func (q Query[S, R]) Where(predicates ...expr.Predicate[S]) Query[S, R] {
	q.source = whereSource(q.source, predicates)
	return q
}
