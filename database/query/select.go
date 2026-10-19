package query

import (
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

type tableSource = querycore.TableSource

type joinKind = querycore.JoinKind

const innerJoin = querycore.InnerJoin

const leftJoin = querycore.LeftJoin

const rightJoin = querycore.RightJoin

const fullJoin = querycore.FullJoin

const crossJoin = querycore.CrossJoin

type joinNode = querycore.JoinNode

type orderNode = querycore.OrderNode

type selectNode = querycore.SelectNode

func orderNodes(orders []queryOrder) []orderNode { return querycore.OrderNodes(orders) }

// requalify is used only after the original model scope has been validated.
// It copies expression containers; the caller's predicates remain unchanged.
func requalify(e expression, alias string) expression {
	switch e := e.(type) {
	case comparison:
		e.Operand = requalifyValue(e.Operand, alias)
		return e
	case subqueryPredicate:
		e.Query = requalifyCorrelation(e.Query, alias)
		if e.Operand != nil {
			e.Operand = requalifyValue(e.Operand, alias)
		}
		return e
	case binaryComparison:
		e.Left, e.Right = requalifyValue(e.Left, alias), requalifyValue(e.Right, alias)
		return e
	case rowComparison:
		operands := make([]valueExpression, len(e.Operands))
		for i, operand := range e.Operands {
			operands[i] = requalifyValue(operand, alias)
		}
		e.Operands = operands
		return e
	case scopeNode:
		return e.Rewrite(func(resolved expression) (expression, error) { return requalify(resolved, alias), nil })
	case junction:
		children := make([]expression, len(e.Children))
		for i, child := range e.Children {
			children[i] = requalify(child, alias)
		}
		e.Children = children
		return e
	case negation:
		return negation{Child: requalify(e.Child, alias)}
	default:
		return e
	}
}

func requalifyValue(v valueExpression, alias string) valueExpression {
	if mapped, ok := mapComputedValue(v, func(v valueExpression) valueExpression { return requalifyValue(v, alias) }, func(p expression) expression { return requalify(p, alias) }); ok {
		return mapped
	}
	switch v := v.(type) {
	case fieldRef:
		v.Table = alias
		return v
	case aggregateNode:
		return requalifyAggregate(v, alias)
	case scalarSubquery:
		v.Query = requalifyCorrelation(v.Query, alias)
		return v
	default:
		return v
	}
}
