package querycore

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestTrigramOperationMetadataAndBounds(t *testing.T) {
	text := OperationArgument{Kind: codec.TypeText, Value: FieldRef{Table: "names", Column: "normalized"}}
	parameter := OperationArgument{Kind: codec.TypeText, Value: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("word")}}
	valid := OperationNode{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat, Arguments: []OperationArgument{text, parameter}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, node := range []OperationNode{
		{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat},
		{Kind: TrigramSimilarityOperation, Result: codec.TypeInteger, Arguments: []OperationArgument{text, parameter}},
		{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat, Arguments: []OperationArgument{{Kind: codec.TypeInteger, Value: text.Value}, parameter}},
		{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat, Arguments: []OperationArgument{text, text}},
		{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat, Arguments: []OperationArgument{text, {Kind: codec.TypeText, Value: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("word"), Literal: true}}}},
		{Kind: TrigramSimilarityOperation, Result: codec.TypeFloat, Arguments: []OperationArgument{text, {Kind: codec.TypeText, Value: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral(int64(2))}}}},
	} {
		if err := node.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid trigram operation accepted", err)
		}
	}
	compiler := NewCompiler([]Column{{Name: "normalized"}})
	compiler.Sources = map[string]map[string]Column{"names": compiler.Columns}
	compiler.ExpressionNodes = MaxExpressionNodes
	if _, err := compiler.SelectedExpression(valid, nil, false); !errors.Is(err, fault.Invalid) || len(compiler.Arguments) != 0 {
		t.Fatal("trigram escaped node budget", err)
	}
	compiler = NewCompiler([]Column{{Name: "normalized"}})
	compiler.Sources = map[string]map[string]Column{"names": compiler.Columns}
	compiler.Arguments = make([]any, MaxParameters)
	if _, err := compiler.SelectedExpression(valid, nil, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("trigram escaped parameter budget", err)
	}
}

func TestTrigramPredicateRequiresTypedTextBinding(t *testing.T) {
	c := NewCompiler([]Column{{Name: "normalized"}})
	c.Sources = map[string]map[string]Column{"names": c.Columns}
	node := Comparison{Operand: FieldRef{Table: "names", Column: "normalized"}, Operator: TrigramMatch, Kind: codec.TypeText, Text: true, Values: RetainComparisonValues([]any{"word"}), Bind: func(v any) (driver.Value, error) { return v, nil }}
	count := 0
	if err := ValidateExpression(node, "names", 0, &count); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []Comparison{
		{Operand: node.Operand, Operator: TrigramMatch, Kind: codec.TypeText, Values: node.Values, Bind: node.Bind},
		{Operand: node.Operand, Operator: TrigramMatch, Kind: codec.TypeInteger, Text: true, Values: node.Values, Bind: node.Bind},
		{Operand: node.Operand, Operator: TrigramMatch, Kind: codec.TypeText, Text: true, Values: node.Values, Bind: func(any) (driver.Value, error) { return int64(1), nil }},
	} {
		if _, err := c.Expression(invalid); !errors.Is(err, fault.Invalid) || len(c.Arguments) != 0 {
			t.Fatal("untyped trigram search accepted", err)
		}
	}
	sql, err := c.Expression(node)
	if err != nil || c.render(sql, false) != `("names"."normalized" OPERATOR(public.%) $1)` || len(c.Arguments) != 1 || c.Arguments[0] != "word" {
		t.Fatal(sql, c.Arguments, err)
	}
}
