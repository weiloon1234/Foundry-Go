package querycore

import (
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

func TestSourceWriteCTEsAvoidPrivateNameCollisions(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "name"}}
	model := ModelDefinition{Table: "archives", Primary: "id", Columns: columns}
	q := QueryCore{Table: "archives", Model: &model}
	input := SelectNode{Source: TableSource{Table: "source_records", Columns: columns}, Selections: ColumnSelections("source_records", columns), Predicates: []Expression{BinaryComparison{Text: true, Left: FieldRef{Table: "source_records", Column: "name"}, Right: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("cte-input")}, Operator: Equal}}}
	definition := &CteNode{Name: "foundry_write_source", Query: input, Columns: columns}
	source := SelectNode{Source: TableSource{Cte: definition, Alias: "foundry_selected_rows", Columns: columns}}
	c := NewCompiler(columns)
	c.Sources = map[string]map[string]Column{"archives": {"id": columns[0], "name": columns[1]}}
	s, err := q.CompileSourceWrite(c, q.ModelSelect(nil), source, SourceWriteKey{Expression: FieldRef{Table: "foundry_selected_rows", Column: "id"}}, UpdateModel, map[string]ValueExpression{"name": FieldRef{Table: "foundry_selected_rows", Column: "name"}}, nil, false)
	if err != nil || !strings.HasPrefix(s.SQLText, `WITH "foundry_write_source"`) || !strings.Contains(s.SQLText, `"foundry_write_source_2"`) || !reflect.DeepEqual(s.Bindings, []any{"cte-input"}) {
		t.Fatal("source dependency/private alias collision", s.SQLText, s.Bindings, err)
	}
}
