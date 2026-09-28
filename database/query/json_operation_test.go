package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestJSONOperationsBindAndValidate(t *testing.T) {
	f := NewJSONField[cursorRecord, value.JSON[[]string]]("records", "rank", codec.JSON[[]string]())
	doc, err := value.NewJSON([]string{"safe'_%"})
	if err != nil {
		t.Fatal(err)
	}
	statement, err := cursorQuery().Where(f.Contains(doc)).Compile()
	if err != nil || !strings.Contains(statement.SQL(), " @> ") || strings.Contains(statement.SQL(), "safe") {
		t.Fatal(statement.SQL(), err)
	}
	if args := statement.Arguments(); len(args) != 2 || args[0] != `["safe'_%"]` || args[1] != true {
		t.Fatal(args)
	}
	badRepresentation := NewJSONField[cursorRecord, value.JSON[[]string]]("records", "rank", codec.JSON[[]string]().WithParameterType(codec.TypeText))
	for _, node := range []valueExpression{
		operationNode{kind: jsonContainsOperation, result: codec.TypeBoolean},
		operationNode{kind: jsonKindOperation, result: codec.TypeInteger, arguments: []operationArgument{operationArg(f.Value())}},
		JSONType(badRepresentation).Value().node,
		JSONType[cursorRecord, value.JSON[[]string]](nil).Value().node,
		JSONContains(f, f.Param(value.JSON[[]string]{})).Value().node,
	} {
		if _, err := SelectValue(cursorQuery().Limit(0), Expression[cursorRecord, bool]{node: node, codec: codec.Bool[bool]()}).Compile(); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := cursorQuery().Where(f.Kind().Eq(JSONKind("typo"))).Compile(); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
}
