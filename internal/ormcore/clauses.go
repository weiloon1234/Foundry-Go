package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func clauseExpression(s *Source, e Expression) {
	s.Err = safeerror.Join("operation failed", s.Err, e.Err, CheckReferenceSet(*s, e.Refs, e.ReferenceFree))
	if e.Node == nil {
		s.Err = safeerror.Join("operation failed", s.Err, Invalid("uninitialized clause expression"))
	}
}

// GroupBy appends immutable row keys; querycore validates row phase and grouping.
func GroupBy(s Source, keys []Expression) Source {
	s.Node.GroupBy = slices.Clone(s.Node.GroupBy)
	for _, key := range keys {
		clauseExpression(&s, key)
		s.Node.GroupBy = append(s.Node.GroupBy, key.Node)
	}
	return s
}

// Having appends immutable group predicates with their full ownership checks.
func Having(s Source, predicates []Predicate) Source {
	s.Node.Having = slices.Clone(s.Node.Having)
	for _, p := range predicates {
		s.Err = safeerror.Join("operation failed", s.Err, p.Err, CheckReferenceSet(s, p.Refs, p.ReferenceFree))
		if p.Node == nil {
			s.Err = safeerror.Join("operation failed", s.Err, Invalid("uninitialized HAVING predicate"))
		}
		s.Node.Having = append(s.Node.Having, p.Node)
	}
	return s
}

// OrderBy appends row/selected orders without permitting model writes.
func OrderBy(s Source, orders []Order) Source {
	s.Node.Orders = slices.Clone(s.Node.Orders)
	for _, o := range orders {
		clauseExpression(&s, Expression{Node: o.Node.Expression, Refs: o.Refs, ReferenceFree: o.ReferenceFree, Err: o.Err})
		s.Node.Orders = append(s.Node.Orders, o.Node)
	}
	return s
}

// Distinct replaces earlier distinct clauses with independently owned keys.
func Distinct(s Source, kind querycore.DistinctKind, keys []Expression) Source {
	nodes := make([]querycore.ValueExpression, len(keys))
	for i, key := range keys {
		clauseExpression(&s, key)
		nodes[i] = key.Node
	}
	s.Node.Distinct = querycore.DistinctSpec{Kind: kind, Keys: nodes}
	return s
}
