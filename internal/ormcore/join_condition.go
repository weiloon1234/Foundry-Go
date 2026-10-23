package ormcore

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// JoinCondition retains independent operand provenance until sources are bound.
type JoinCondition struct {
	Node                querycore.Expression
	Left, Right         []Reference
	LeftFree, RightFree bool
	Err                 error
}

// JoinComparison shares expression validation with ordinary row comparisons.
func JoinComparison(left, right Expression, op querycore.Operator) JoinCondition {
	p := CompareExpressions(left, right, op)
	return JoinCondition{Node: p.Node, Left: left.Refs, Right: right.Refs, LeftFree: left.ReferenceFree, RightFree: right.ReferenceFree, Err: p.Err}
}

// CombineJoinConditions snapshots a bounded immutable AND/OR condition list.
func CombineJoinConditions(any bool, conditions []JoinCondition) JoinCondition {
	out := JoinCondition{LeftFree: true, RightFree: true}
	if len(conditions) > querycore.MaxExpressionNodes {
		out.Err = Invalid("join conditions exceed their resource bound")
		return out
	}
	children := make([]querycore.Expression, len(conditions))
	var left, right referenceSet
	for i, c := range conditions {
		children[i] = c.Node
		left.add(c.Left)
		right.add(c.Right)
		out.LeftFree = out.LeftFree && c.LeftFree
		out.RightFree = out.RightFree && c.RightFree
		out.Err = errors.Join(out.Err, c.Err)
		if c.Node == nil {
			out.Err = errors.Join(out.Err, Invalid("uninitialized join condition"))
		}
	}
	out.Left, out.Right = left.values, right.values
	out.Err = errors.Join(out.Err, left.err, right.err)
	out.Node = querycore.Junction{Any: any, Children: children}
	return out
}

// NegateJoinCondition retains both sides while negating SQL three-valued truth.
func NegateJoinCondition(on JoinCondition) JoinCondition {
	if on.Node == nil {
		on.Err = errors.Join(on.Err, Invalid("uninitialized join condition"))
	}
	on.Node = querycore.Negation{Child: on.Node}
	return on
}

// FilterJoinCondition adds predicates owned by just one side of an ON boundary.
func FilterJoinCondition(on JoinCondition, left bool, predicates []Predicate) JoinCondition {
	if len(predicates) > querycore.MaxExpressionNodes {
		on.Err = errors.Join(on.Err, Invalid("join filters exceed their resource bound"))
		return on
	}
	conditions := make([]JoinCondition, 1, len(predicates)+1)
	conditions[0] = on
	for _, p := range predicates {
		c := JoinCondition{Node: p.Node, LeftFree: true, RightFree: true, Err: p.Err}
		if left {
			c.Left, c.LeftFree = p.Refs, p.ReferenceFree
		} else {
			c.Right, c.RightFree = p.Refs, p.ReferenceFree
		}
		conditions = append(conditions, c)
	}
	return CombineJoinConditions(false, conditions)
}
