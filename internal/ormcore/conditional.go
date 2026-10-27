package ormcore

import (
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"slices"

	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

// Conditional composes values without erasing any operand's errors or ownership.
func Conditional(kind querycore.ConditionalKind, arguments ...Expression) Expression {
	out := Expression{ReferenceFree: true}
	if err := querycore.ValidateConditionalArguments(kind, len(arguments)); err != nil {
		out.Err = err
		return out
	}
	nodes := make([]querycore.ValueExpression, len(arguments))
	var refs referenceSet
	for i, input := range arguments {
		nodes[i] = input.Node
		refs.add(input.Refs)
		out.ReferenceFree = out.ReferenceFree && input.ReferenceFree
		out.Err = safeerror.Join("operation failed", out.Err, input.Err)
		if input.Node == nil {
			out.Err = safeerror.Join("operation failed", out.Err, Invalid("uninitialized conditional operand"))
		}
	}
	out.Refs = refs.values
	out.Err = safeerror.Join("operation failed", out.Err, refs.err)
	out.Node = querycore.ConditionalNode{Kind: kind, Arguments: nodes}
	return out
}

// CaseBuilder retains immutable branches and their complete ownership metadata.
// Its zero value acquires its first branch through Append.
type CaseBuilder struct {
	branches      []querycore.CaseBranch
	refs          []Reference
	referenceFree bool
	err           error
}

// Empty reports whether the first branch still needs to supply the result codec.
func (c CaseBuilder) Empty() bool { return len(c.branches) == 0 }

// Append returns an independent CASE derivation with bounded branch/reference work.
func (c CaseBuilder) Append(condition Predicate, result Expression) CaseBuilder {
	if len(c.branches) >= querycore.MaxExpressionNodes {
		c.err = safeerror.Join("operation failed", c.err, Invalid("CASE exceeds its branch bound"))
		return c
	}
	referenceFree := c.referenceFree || c.Empty()
	refs, err := MergeReferences(c.refs, condition.Refs, result.Refs)
	c.err = safeerror.Join("operation failed", c.err, condition.Err, result.Err, err)
	if condition.Node == nil || result.Node == nil {
		c.err = safeerror.Join("operation failed", c.err, Invalid("uninitialized CASE branch"))
	}
	c.branches = append(slices.Clone(c.branches), querycore.CaseBranch{Condition: condition.Node, Result: result.Node})
	c.refs = refs
	c.referenceFree = referenceFree && condition.ReferenceFree && result.ReferenceFree
	return c
}

// Finish requires a branch and an explicit ELSE, preserving fallback provenance.
func (c CaseBuilder) Finish(otherwise Expression) Expression {
	refs, err := MergeReferences(c.refs, otherwise.Refs)
	err = safeerror.Join("operation failed", c.err, otherwise.Err, err)
	if c.Empty() || otherwise.Node == nil {
		err = safeerror.Join("operation failed", err, Invalid("CASE requires a branch and an explicit ELSE"))
	}
	return Expression{Node: querycore.CaseNode{Branches: c.branches, Otherwise: otherwise.Node}, Refs: refs, ReferenceFree: c.referenceFree && otherwise.ReferenceFree, Err: err}
}
