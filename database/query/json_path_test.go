package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONPathParametersAndBoundaries(t *testing.T) {
	field := NewJSONField[cursorRecord, value.JSON[map[string][]string]]("records", "rank", codec.JSON[map[string][]string]())
	root := JSONRoot(field)
	property := NewJSONProperty[cursorRecord, map[string][]string, []string](root, "odd'_%", false)
	element := NewJSONArrayElement[cursorRecord, []string, string](property, -1)
	text := JSONTextScalar(element, codec.String[string]())
	statement, err := cursorQuery().Where(text.Like("%go%")).Compile()
	// Declared property names are escaped literals so expression indexes match;
	// run-time indices, map keys and comparison values stay parameters.
	if err != nil || !strings.Contains(statement.SQL(), `-> CAST(CAST(E'odd''_%' AS text) AS text)`) || strings.Contains(statement.SQL(), "%go%") {
		t.Fatal(statement.SQL(), err)
	}
	args := statement.Arguments()
	if len(args) != 2 || args[0] != int64(-1) || args[1] != "%go%" {
		t.Fatal(args)
	}
	entry := NewJSONMapEntry[cursorRecord, map[string][]string, []string](root, "runtime'key")
	keyed, err := cursorQuery().Where(entry.Exists()).Compile()
	if err != nil || strings.Contains(keyed.SQL(), "runtime") || len(keyed.Arguments()) != 1 || keyed.Arguments()[0] != "runtime'key" {
		t.Fatal("run-time map key was not bound", keyed.SQL(), err)
	}
	if !strings.Contains(statement.SQL(), " AS integer)") {
		t.Fatal("array index is not a PostgreSQL integer", statement.SQL())
	}
	for _, bad := range []JSONPath[cursorRecord, string]{
		{},
		NewJSONProperty[cursorRecord, map[string][]string, string](root, "\x00", false),
		NewJSONProperty[cursorRecord, map[string][]string, string](root, "\xff", false),
		NewJSONProperty[cursorRecord, map[string][]string, string](root, strings.Repeat("x", value.JSONMaxBytes+1), false),
		NewJSONMapEntry[cursorRecord, map[string][]string, string](root, "\x00"),
	} {
		if _, err := cursorQuery().Limit(0).Where(bad.Exists()).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestJSONPathInvalidOperationMetadata(t *testing.T) {
	input := parameterExpression[cursorRecord]("x", codec.String[string]()).Value()
	for _, node := range []operationNode{
		{kind: jsonPropertyOperation, result: codec.TypeJSON, arguments: []operationArgument{operationArg(input), operationArg(input)}},
		{kind: jsonScalarOperation, result: codec.TypeJSON, arguments: []operationArgument{{input.node, codec.TypeJSON}}},
		{kind: jsonScalarOperation, result: codec.TypeBytes, arguments: []operationArgument{{input.node, codec.TypeJSON}}},
		{kind: jsonIndexOperation, result: codec.TypeJSON, arguments: []operationArgument{{input.node, codec.TypeJSON}, operationArg(input)}},
	} {
		if err := node.validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
}
