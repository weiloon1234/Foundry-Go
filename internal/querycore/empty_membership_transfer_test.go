package querycore

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestEmptyMembershipValidatesDiscardedOperand(t *testing.T) {
	node := structuralSelect("records")
	for _, operand := range []ValueExpression{FieldRef{Table: "records", Column: "missing"}, ParameterNode{Kind: codec.TypeText, Err: fault.New(fault.Invalid, "invalid captured parameter")}} {
		for _, operator := range []Operator{In, NotIn} {
			comparison := Comparison{Operand: operand, Operator: operator}
			c := NewCompiler([]Column{{Name: "id"}})
			c.Sources = map[string]map[string]Column{"records": c.Columns}
			if _, err := c.Comparison(comparison, nil, false); !errors.Is(err, fault.Invalid) {
				t.Fatal("empty membership skipped operand compilation", err)
			}
			node.Predicates = []Expression{comparison}
			if s, err := CompileRead(node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) || s.SQLText != "" || len(s.Bindings) != 0 {
				t.Fatal("empty IN concealed invalid operand", s.SQLText, err)
			}
		}
	}
}

func TestEmptyMembershipDiscardsComputedBindings(t *testing.T) {
	node := structuralSelect("records")
	id := FieldRef{Table: "records", Column: "id"}
	param := func(n int64) ParameterNode { return ParameterNode{Kind: codec.TypeInteger, Value: RetainLiteral(n)} }
	choice := CaseNode{Branches: []CaseBranch{{Condition: BinaryComparison{Left: id, Right: param(1), Operator: Greater}, Result: param(2)}}, Otherwise: param(3)}
	node.Selections = []SelectItem{{Expression: param(6)}}
	node.Predicates = []Expression{BinaryComparison{Left: id, Right: param(4), Operator: Equal}, Negation{Child: Comparison{Operand: choice, Operator: In, Bind: func(v any) (driver.Value, error) { return v, nil }}}, BinaryComparison{Left: id, Right: param(5), Operator: Equal}}
	s, err := CompileRead(node, t.Context(), false, nil, true)
	if err != nil || !reflect.DeepEqual(s.Bindings, []any{int64(6), int64(4), int64(5)}) || strings.Contains(s.SQLText, "CASE") || !strings.Contains(s.SQLText, `"records"."id" = CAST($3 AS bigint)`) {
		t.Fatal("empty membership retained operand bindings or left a placeholder hole", s.SQLText, s.Bindings, err)
	}
}

func TestEmptyJunctionAndFloatingMembershipSQL(t *testing.T) {
	c := NewCompiler([]Column{{Name: "id"}})
	c.Sources = map[string]map[string]Column{"records": c.Columns}
	for _, test := range []struct {
		node Expression
		want string
	}{{Junction{}, "TRUE"}, {Junction{Any: true}, "FALSE"}} {
		got, err := c.Expression(test.node)
		if err != nil || got != test.want {
			t.Fatal(got, err)
		}
	}
	got, err := c.Expression(Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: NotIn, Values: RetainComparisonValues([]any{0.5}), Bind: func(v any) (driver.Value, error) { return v, nil }, Kind: codec.TypeFloat})
	if err != nil || got != `("records"."id" NOT IN ($1))` || !reflect.DeepEqual(c.Arguments, []any{0.5}) {
		t.Fatal(got, c.Arguments, err)
	}
}
