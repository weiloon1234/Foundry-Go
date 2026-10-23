package querycore

import (
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/statementmeta"
)

func TestCompilerStatementShape(t *testing.T) {
	for _, kind := range []codec.ParameterType{0, codec.TypeText} {
		var previous string
		for _, n := range []int{1, 3, 20} {
			c := NewCompiler([]Column{{Name: "id"}})
			c.TrackShape()
			values := make([]any, n)
			for i := range values {
				values[i] = "canary-private"
			}
			calls := 0
			sql, err := c.Expression(Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: In, Values: values, Kind: kind, Bind: func(v any) (driver.Value, error) { calls++; return v.(string), nil }})
			if err != nil {
				t.Fatal(err)
			}
			s := c.Statement(sql)
			if calls != n || strings.Contains(s.Shape, "canary") || strings.ContainsRune(s.SQLText, 0) || strings.Contains(s.Shape, "$") {
				t.Fatalf("invalid rendering: %+v, binds %d", s, calls)
			}
			if previous != "" && previous != s.Shape {
				t.Fatalf("arity changes shape: %s / %s", previous, s.Shape)
			}
			previous = s.Shape
		}
	}
}

func TestCompilerFragmentsPreserveValuesAndContainers(t *testing.T) {
	c := NewCompiler(nil)
	c.TrackShape()
	scalar, _ := c.Parameter("canary-private")
	literal, _ := c.SchemaLiteral("canary-schema")
	array := "ARRAY[" + scalar + "]"
	one := c.Statement("VALUES (" + scalar + ")")
	many := c.Statement("VALUES (" + array + ")")
	if one.Shape == many.Shape {
		t.Fatal("array merged with scalar")
	}
	s := c.Statement("SELECT " + scalar + ", " + literal)
	if s.SQLText != "SELECT $1, E'canary-schema'" || s.Shape != "SELECT ?, ?" || !reflect.DeepEqual(s.Bindings, []any{"canary-private"}) {
		t.Fatalf("%+v", s)
	}
}

func TestCompilerInsertBatchShape(t *testing.T) {
	columns := []Column{{Name: "id"}, {Name: "name"}}
	q := QueryCore{Table: "records", ScopeContext: statementmeta.Enable(t.Context()), Model: &ModelDefinition{Columns: columns, Primary: "id"}}
	var shape string
	for _, n := range []int{1, 3, 20} {
		rows := make([]map[string]MutationAssignment, n)
		for i := range rows {
			rows[i] = map[string]MutationAssignment{
				"id":   {AssignmentValue: AssignmentValue{Bind: func() (driver.Value, error) { return int64(1), nil }}},
				"name": {AssignmentValue: AssignmentValue{Bind: func() (driver.Value, error) { return "canary-private", nil }}},
			}
		}
		s, err := q.CompileInsert(NewCompiler(columns), rows, map[string]bool{"id": true, "name": true}, SelectNode{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Bindings) != 2*n || strings.Contains(s.Shape, "canary") || strings.ContainsRune(s.SQLText, 0) {
			t.Fatalf("%+v", s)
		}
		if shape != "" && shape != s.Shape {
			t.Fatalf("batch length changes shape: %s / %s", shape, s.Shape)
		}
		shape = s.Shape
		if !strings.Contains(shape, "VALUES (?, ?)") {
			t.Fatal(shape)
		}
	}
}
