package ormcore

import (
	"errors"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// OrderExpression retains an expression's provenance without specializing by
// model, result type or row/selected phase. Typed shells supply the phase.
func OrderExpression(e Expression, descending bool) Order {
	return Order{Node: querycore.OrderNode{Expression: e.Node, Descending: descending}, Refs: e.Refs, ReferenceFree: e.ReferenceFree, Err: e.Err}
}

// OperationArgument retains a scalar operand's representation and provenance.
type OperationArgument struct {
	Value Expression
	Kind  codec.ParameterType
}

// Operation combines immutable typed operands in the shared scalar AST.
func Operation(kind querycore.ScalarOperation, result codec.ParameterType, arguments ...OperationArgument) Expression {
	out := Expression{ReferenceFree: true}
	var refs referenceSet
	nodes := make([]querycore.OperationArgument, len(arguments))
	for i, argument := range arguments {
		input := argument.Value
		nodes[i] = querycore.OperationArgument{Value: input.Node, Kind: argument.Kind}
		refs.add(input.Refs)
		out.ReferenceFree = out.ReferenceFree && input.ReferenceFree
		out.Err = errors.Join(out.Err, input.Err)
		if input.Node == nil {
			out.Err = errors.Join(out.Err, Invalid("uninitialized expression"))
		}
	}
	out.Refs = refs.values
	out.Err = errors.Join(out.Err, refs.err)
	node := querycore.OperationNode{Kind: kind, Arguments: nodes, Result: result}
	out.Err = errors.Join(out.Err, node.Validate())
	out.Node = node
	return out
}

// CompareExpressions preserves both operands' declaration provenance.
func CompareExpressions(left, right Expression, op querycore.Operator, text ...bool) Predicate {
	refs, referenceErr := MergeReferences(left.Refs, right.Refs)
	err := errors.Join(left.Err, right.Err, referenceErr)
	if left.Node == nil || right.Node == nil {
		err = errors.Join(err, Invalid("uninitialized comparison operand"))
	}
	return Predicate{Node: querycore.BinaryComparison{Left: left.Node, Right: right.Node, Operator: op, Text: len(text) == 1 && text[0]}, Refs: refs, ReferenceFree: left.ReferenceFree && right.ReferenceFree, Err: err}
}

// Conjunction joins conditions without duplicating retained ownership references.
func Conjunction(predicates []Predicate) Predicate {
	out := Predicate{ReferenceFree: true}
	if len(predicates) > querycore.MaxExpressionNodes {
		out.Err = Invalid("conjunction exceeds its resource bound")
		return out
	}
	children := make([]querycore.Expression, len(predicates))
	var refs referenceSet
	for i, p := range predicates {
		children[i] = p.Node
		refs.add(p.Refs)
		out.ReferenceFree = out.ReferenceFree && p.ReferenceFree
		out.Err = errors.Join(out.Err, p.Err)
		if p.Node == nil {
			out.Err = errors.Join(out.Err, Invalid("uninitialized predicate"))
		}
	}
	out.Node = querycore.Junction{Children: children}
	out.Refs = refs.values
	out.Err = errors.Join(out.Err, refs.err)
	return out
}

// NegatePredicate retains ownership and errors under SQL NOT.
func NegatePredicate(p Predicate) Predicate {
	if p.Node == nil {
		p.Err = errors.Join(p.Err, Invalid("uninitialized predicate"))
	}
	p.Node = querycore.Negation{Child: p.Node}
	return p
}
