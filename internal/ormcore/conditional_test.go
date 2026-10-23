package ormcore

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestConditionalBoundsAndCaseOwnership(t *testing.T) {
	for _, out := range []Expression{
		Conditional(querycore.CoalesceValues),
		Conditional(querycore.CoalesceValues, make([]Expression, querycore.MaxExpressionNodes+1)...),
		Conditional(querycore.NullIfValues, Expression{}),
		Conditional(querycore.ConditionalKind(99), Expression{}),
		(CaseBuilder{branches: make([]querycore.CaseBranch, querycore.MaxExpressionNodes)}).Append(Predicate{}, Expression{}).Finish(Expression{}),
	} {
		if !errors.Is(out.Err, fault.Invalid) {
			t.Fatal("unbounded/incomplete conditional accepted", out.Err)
		}
	}
	owner := new(int)
	ref := Reference{Table: "records", Owner: owner}
	operand := Expression{Node: querycore.ParameterNode{}, Refs: []Reference{ref}, ReferenceFree: true}
	predicate := Predicate{Node: querycore.Junction{}, Refs: []Reference{ref}, ReferenceFree: true}
	builder := CaseBuilder{}
	for range 64 {
		operand = Conditional(querycore.CoalesceValues, operand, operand)
		builder = builder.Append(predicate, operand)
		out := builder.Finish(operand)
		if out.Err != nil || len(out.Refs) != 1 || !out.ReferenceFree {
			t.Fatal("conditional reference growth", out.Err, len(out.Refs))
		}
	}
	// A column reference in any condition/branch/fallback prevents a composed
	// expression from claiming it is reference-free.
	predicate.ReferenceFree = false
	out := builder.Append(predicate, operand).Finish(operand)
	if out.ReferenceFree {
		t.Fatal("CASE discarded column dependencies")
	}
}
