package ormcore

import (
	"errors"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Aggregate constructs a stored-field aggregate retaining declaration provenance.
func Aggregate(input Expression, kind querycore.AggregateKind, cast querycore.AggregateCast) Expression {
	field, ok := input.Node.(querycore.FieldRef)
	err := input.Err
	if !ok {
		err = errors.Join(err, Invalid("aggregate requires a stored field"))
	}
	node := querycore.AggregateNode{Kind: kind, Cast: cast, Field: field, Sensitive: input.Sensitive}
	err = errors.Join(err, node.Validate(func(querycore.FieldRef) error { return nil }))
	return Expression{Node: node, Refs: input.Refs, ReferenceFree: input.ReferenceFree, Err: err}
}

// FilterAggregate appends immutable row filters while preserving all ownership.
func FilterAggregate(input Expression, predicates []Predicate) Expression {
	if len(predicates) == 0 {
		return input
	}
	node, ok := input.Node.(querycore.AggregateNode)
	if !ok {
		input.Err = errors.Join(input.Err, Invalid("uninitialized aggregate"))
		return input
	}
	var previous []querycore.Expression
	if node.Filter != nil {
		previous = node.Filter.Predicates
	}
	if err := querycore.ValidateAggregateFilterCount(len(previous) + len(predicates)); err != nil {
		input.Err = errors.Join(input.Err, err)
		return input
	}
	children := slices.Clone(previous)
	refs := referenceSet{}
	refs.add(input.Refs)
	for _, p := range predicates {
		children = append(children, p.Node)
		refs.add(p.Refs)
		input.ReferenceFree = input.ReferenceFree && p.ReferenceFree
		input.Err = errors.Join(input.Err, p.Err)
		if p.Node == nil {
			input.Err = errors.Join(input.Err, Invalid("uninitialized aggregate filter"))
		}
	}
	input.Refs = refs.values
	input.Err = errors.Join(input.Err, refs.err)
	node.Filter = &querycore.AggregateFilter{Predicates: children}
	input.Node = node
	return input
}
