package ormcore

import (
	"errors"
	"slices"
	"sort"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// SelectSubquery retains lazy declaration checks at the shared SELECT boundary.
// It uses the same Subquery/RelationCheck owner as relation EXISTS.
func SelectSubquery(source Source) querycore.Subquery {
	err := source.Err
	if source.Scope == nil || len(source.Owners) == 0 {
		err = errors.Join(err, Invalid("subquery requires a declared source"))
	}
	node := source.Node
	// Ordinary aliases select their complete record; joined existence uses SELECT 1.
	if len(node.Selections) == 0 && len(node.Joins) == 0 {
		node.Selections = querycore.ColumnSelections(node.Source.Name(), node.Source.Columns)
	}
	checks := AppendChecks(source.Checks)
	return querycore.Subquery{Node: node, Err: err, RelationCheck: func() error {
		for _, check := range checks {
			if check != nil {
				if err := check(); err != nil {
					return err
				}
			}
		}
		return nil
	}}
}

// SelectScalar validates exactly one typed selection before building a subquery.
func SelectScalar(source Source, value Expression) Source {
	source.Err = errors.Join(source.Err, value.Err, CheckReferenceSet(source, value.Refs, value.ReferenceFree))
	if value.Node == nil {
		source.Err = errors.Join(source.Err, Invalid("scalar selection requires a value"))
	}
	source.Node.Selections = []querycore.SelectItem{{Expression: value.Node, Alias: "value", Sensitive: value.Sensitive}}
	return source
}

// AnchorExpression makes an independent value retain the concrete outer source.
func AnchorExpression(source Source) Expression {
	out := Expression{ReferenceFree: true, Err: source.Err}
	if source.Scope == nil || len(source.Owners) == 0 || len(source.Owners) > querycore.MaxExpressionNodes {
		out.Err = errors.Join(out.Err, Invalid("subquery requires a declared outer scope"))
		return out
	}
	names := make([]string, 0, len(source.Owners))
	for name := range source.Owners {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.Refs = append(out.Refs, Reference{Scope: source.Scope, Table: name, Owner: source.Owners[name]})
	}
	return out
}

// ScalarExpression is a SELECT boundary, independent of its outer aggregate phase.
func ScalarExpression(outer Source, inner Source) Expression {
	out := AnchorExpression(outer)
	out.Err = errors.Join(out.Err, inner.Err)
	inner.Checks = AppendChecks(inner.Checks, outer.Checks...)
	query := SelectSubquery(inner)
	out.Err = errors.Join(out.Err, query.Err)
	out.Node = querycore.ScalarSubquery{Query: query}
	return out
}

// ExistsPredicate shares SubqueryPredicate/SubquerySQL with relation existence.
func ExistsPredicate(outer Source, inner Source) Predicate {
	anchor := AnchorExpression(outer)
	inner.Checks = AppendChecks(inner.Checks, outer.Checks...)
	return Predicate{Node: querycore.SubqueryPredicate{Query: SelectSubquery(inner)}, Refs: anchor.Refs, ReferenceFree: true, Err: anchor.Err}
}

// MembershipPredicate preserves SQL NULL behavior and only the outer row references.
func MembershipPredicate(left Expression, inner Source) Predicate {
	err := left.Err
	if left.Node == nil {
		err = errors.Join(err, Invalid("membership requires a row value"))
	}
	return Predicate{Node: querycore.SubqueryPredicate{Operand: left.Node, Query: SelectSubquery(inner)}, Refs: left.Refs, ReferenceFree: left.ReferenceFree, Err: err}
}

// WhereSource owns immutable predicate appends for projected and scalar queries.
func WhereSource(source Source, predicates []Predicate) Source {
	source.Node.Predicates = slices.Clone(source.Node.Predicates)
	for _, p := range predicates {
		source.Err = errors.Join(source.Err, p.Err, CheckReferenceSet(source, p.Refs, p.ReferenceFree))
		source.Node.Predicates = append(source.Node.Predicates, p.Node)
	}
	return source
}
