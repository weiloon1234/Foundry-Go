package querycore

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
)

func TestJSONPathInvalidOperationMetadata(t *testing.T) {
	input := ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("x")}
	text := OperationArgument{Value: input, Kind: codec.TypeText}
	json := OperationArgument{Value: input, Kind: codec.TypeJSON}
	for _, node := range []OperationNode{
		{Kind: JsonPropertyOperation, Result: codec.TypeJSON, Arguments: []OperationArgument{text, text}},
		{Kind: JsonScalarOperation, Result: codec.TypeJSON, Arguments: []OperationArgument{json}},
		{Kind: JsonScalarOperation, Result: codec.TypeBytes, Arguments: []OperationArgument{json}},
		{Kind: JsonIndexOperation, Result: codec.TypeJSON, Arguments: []OperationArgument{json, text}},
		{Kind: JsonContainsOperation, Result: codec.TypeBoolean},
		{Kind: JsonKindOperation, Result: codec.TypeInteger, Arguments: []OperationArgument{json}},
	} {
		if err := node.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid JSON operation metadata", err)
		}
	}
	for _, name := range []string{"\x00", "\xff", strings.Repeat("x", value.JSONMaxBytes+1)} {
		if err := ValidateJSONProperty(name); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid JSON property accepted", err)
		}
	}
}

func TestJSONDeclaredPropertyEscapesLiteralWithoutBinding(t *testing.T) {
	c := NewCompiler([]Column{{Name: "document"}})
	c.Sources = map[string]map[string]Column{"records": c.Columns}
	node := OperationNode{Kind: JsonPropertyOperation, Result: codec.TypeJSON, Arguments: []OperationArgument{
		{Kind: codec.TypeJSON, Value: FieldRef{Table: "records", Column: "document"}},
		{Kind: codec.TypeText, Value: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("odd'_%"), Literal: true}},
	}}
	sql, err := c.SelectedExpression(node, nil, false)
	if err != nil || !strings.Contains(sql, `-> CAST(CAST(E'odd''_%' AS text) AS text)`) || len(c.Arguments) != 0 {
		t.Fatal(sql, c.Arguments, err)
	}
}
