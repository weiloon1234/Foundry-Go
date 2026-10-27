package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"maps"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// VisibleScope carries the SQL visibility and declaration identities together.
type VisibleScope struct {
	Scope    reflect.Type
	Required querycore.ScopeRequirement
	Owners   map[string]any
	Checks   []func() error
}

// Correlation retains the inner SELECT and its enclosing visibility separately.
type Correlation struct {
	Source       Source
	Outer, Inner VisibleScope
}

func SourceVisibility(source Source) VisibleScope {
	r := VisibleScope{Scope: source.Scope, Required: querycore.ScopeRequirement{Sources: map[string][]querycore.Column{}, Err: source.Err}, Owners: maps.Clone(source.Owners), Checks: AppendChecks(source.Checks)}
	if source.Scope == nil || len(source.Node.Joins) > querycore.MaxExpressionNodes {
		r.Required.Err = safeerror.Join("operation failed", r.Required.Err, Invalid("correlation requires a bounded declared scope"))
		return r
	}
	add := func(table querycore.TableSource) {
		name := table.Name()
		if name == "" || r.Owners[name] == nil || r.Required.Sources[name] != nil {
			r.Required.Err = safeerror.Join("operation failed", r.Required.Err, Invalid("correlation requires distinct owned sources"))
		}
		r.Required.Sources[name] = slices.Clone(table.Columns)
	}
	add(source.Node.Source)
	for _, j := range source.Node.Joins {
		add(j.Source)
	}
	return r
}

func BindCorrelation(outer VisibleScope, inner Source, outerType, scope reflect.Type) Correlation {
	r := Correlation{Source: inner, Outer: outer, Inner: SourceVisibility(inner)}
	r.Source.Scope = scope
	r.Source.Owners = maps.Clone(outer.Owners)
	if r.Source.Owners == nil {
		r.Source.Owners = map[string]any{}
	}
	maps.Copy(r.Source.Owners, inner.Owners)
	r.Source.Checks = AppendChecks(outer.Checks, inner.Checks...)
	r.Source.Err = safeerror.Join("operation failed", inner.Err, outer.Required.Err, r.Inner.Required.Err)
	if outer.Scope != outerType || len(outer.Required.Sources) == 0 {
		r.Source.Err = safeerror.Join("operation failed", r.Source.Err, Invalid("correlation requires its declared outer scope"))
	}
	for name := range r.Inner.Required.Sources {
		if _, ok := outer.Required.Sources[name]; ok {
			r.Source.Err = safeerror.Join("operation failed", r.Source.Err, Invalid("correlation shadows an outer source; use distinct aliases"))
		}
	}
	return r
}

func CorrelationVisibility(c Correlation) VisibleScope {
	r := VisibleScope{Scope: c.Source.Scope, Owners: maps.Clone(c.Source.Owners), Checks: AppendChecks(c.Source.Checks), Required: querycore.ScopeRequirement{Sources: maps.Clone(c.Outer.Required.Sources), Err: c.Source.Err}}
	if r.Required.Sources == nil {
		r.Required.Sources = map[string][]querycore.Column{}
	}
	maps.Copy(r.Required.Sources, c.Inner.Required.Sources)
	return r
}

func CorrelationScope(c Correlation, scope Scope, outer bool) Scope {
	side := c.Inner
	if outer {
		side = c.Outer
	}
	scope.Err = safeerror.Join("operation failed", scope.Err, c.Source.Err, side.Required.Err)
	if scope.Scope != nil && scope.Scope != side.Scope {
		scope.Err = safeerror.Join("operation failed", scope.Err, Invalid("scope belongs to another correlation boundary"))
	}
	scope = Rescope(scope, side.Owners, scope.Err)
	if side.Required.Sources[scope.Table] == nil {
		scope.Err = safeerror.Join("operation failed", scope.Err, Invalid("scope is outside this correlation side"))
	}
	scope.Scope = c.Source.Scope
	return scope
}

func CorrelationQuery(c Correlation) querycore.Subquery {
	q := SelectSubquery(c.Source)
	// An unselected correlated source uses SELECT 1 for existence. Explicit
	// selections remain intact, including aggregates whose cardinality matters.
	q.Node.Selections = c.Source.Node.Selections
	required := c.Outer.Required
	if c.Outer.Scope == nil || len(required.Sources) == 0 {
		q.Err = safeerror.Join("operation failed", q.Err, Invalid("correlated query requires an enclosing scope"))
	}
	q.Err = safeerror.Join("operation failed", q.Err, required.Err)
	q.Correlation = &required
	return q
}

func correlationAnchor(c Correlation) Expression {
	return AnchorExpression(Source{Scope: c.Outer.Scope, Owners: c.Outer.Owners, Err: safeerror.Join("operation failed", c.Source.Err, c.Outer.Required.Err)})
}

func CorrelatedExists(c Correlation) Predicate {
	anchor := correlationAnchor(c)
	q := CorrelationQuery(c)
	return Predicate{Node: querycore.SubqueryPredicate{Query: q}, Refs: anchor.Refs, ReferenceFree: anchor.ReferenceFree, Err: safeerror.Join("operation failed", anchor.Err, q.Err)}
}

func CorrelatedMembership(left Expression, c Correlation) Predicate {
	p := CorrelatedExists(c)
	p.Err = safeerror.Join("operation failed", p.Err, left.Err, CheckReferenceSet(Source{Scope: c.Outer.Scope, Owners: c.Outer.Owners}, left.Refs, left.ReferenceFree))
	if left.Node == nil {
		p.Err = safeerror.Join("operation failed", p.Err, Invalid("correlated membership requires a row value"))
	}
	p.Node = querycore.SubqueryPredicate{Query: CorrelationQuery(c), Operand: left.Node}
	return p
}

func CorrelatedScalarExpression(c Correlation) Expression {
	out := correlationAnchor(c)
	q := CorrelationQuery(c)
	out.Err = safeerror.Join("operation failed", out.Err, q.Err)
	out.Node = querycore.ScalarSubquery{Query: q}
	return out
}

func OrderSource(source Source, orders []Order) Source {
	source.Node.Orders = slices.Clone(source.Node.Orders)
	for _, o := range orders {
		source.Err = safeerror.Join("operation failed", source.Err, o.Err, CheckReferenceSet(source, o.Refs, o.ReferenceFree))
		source.Node.Orders = append(source.Node.Orders, o.Node)
	}
	return source
}

// CorrelatedRecord contains complete result metadata without an executable query.
type CorrelatedRecord struct {
	Correlation Correlation
	Columns     []querycore.Column
	Owner       any
}

func SelectCorrelationRecord(c Correlation, scope Scope) CorrelatedRecord {
	c.Source.Err = safeerror.Join("operation failed", c.Source.Err, scope.Err)
	if scope.Scope != c.Source.Scope || c.Inner.Owners[scope.Table] != scope.Owner || scope.Owner == nil {
		c.Source.Err = safeerror.Join("operation failed", c.Source.Err, Invalid("correlated record requires a preserved inner scope"))
	}
	columns := slices.Clone(c.Inner.Required.Sources[scope.Table])
	if len(columns) == 0 {
		c.Source.Err = safeerror.Join("operation failed", c.Source.Err, Invalid("correlated record requires declared columns"))
	}
	c.Source.Node.Selections = querycore.ColumnSelections(scope.Table, columns)
	return CorrelatedRecord{Correlation: c, Columns: columns, Owner: scope.Owner}
}

func AliasLateral(record CorrelatedRecord, alias string, scope reflect.Type) Source {
	q := CorrelationQuery(record.Correlation)
	err := q.Err
	if record.Owner == nil || len(record.Columns) == 0 {
		err = safeerror.Join("operation failed", err, Invalid("lateral requires a complete correlated record"))
	}
	node := querycore.AliasNode(q.Node, slices.Clone(record.Columns), false, alias, scope, err)
	return Source{Scope: scope, Node: node.Node, Aliases: node.Aliases, Owners: map[string]any{alias: record.Owner}, Err: node.Err, Checks: AppendChecks(record.Correlation.Source.Checks)}
}

func JoinLateral(left, right Source, outer VisibleScope, on []JoinCondition, kind querycore.JoinKind, scope reflect.Type) Source {
	anchor := AnchorExpression(Source{Scope: outer.Scope, Owners: outer.Owners, Err: outer.Required.Err})
	right.Err = safeerror.Join("operation failed", right.Err, anchor.Err, CheckReferenceSet(left, anchor.Refs, anchor.ReferenceFree))
	if len(outer.Required.Sources) == 0 || right.Node.Source.Query == nil || (kind != querycore.CrossJoin && kind != querycore.InnerJoin && kind != querycore.LeftJoin) {
		right.Err = safeerror.Join("operation failed", right.Err, Invalid("lateral requires a preceding outer source and a derived cross, inner or left join"))
	}
	condition := JoinCondition{LeftFree: true, RightFree: true}
	if len(on) != 0 {
		condition = CombineJoinConditions(false, on)
	}
	result := JoinSources(left, right, condition, kind, scope)
	if result.Err == nil {
		required := outer.Required
		result.Node.Joins[len(result.Node.Joins)-1].Lateral = &required
	}
	return result
}
