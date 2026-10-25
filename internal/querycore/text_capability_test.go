package querycore

import (
	"database/sql/driver"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"strings"
	"testing"
)

func TestPatternComparisonRequiresTextCapability(t *testing.T) {
	for _, op := range []Operator{Like, InsensitiveLike, Contains, InsensitiveContains, StartsWith, EndsWith, InsensitiveStartsWith, InsensitiveEndsWith} {
		c := NewCompiler([]Column{{Name: "state"}})
		binds := 0
		node := Comparison{Operand: FieldRef{Table: "items", Column: "state"}, Operator: op, Kind: codec.TypeText, Values: RetainComparisonValues([]any{"private"}), Bind: func(v any) (driver.Value, error) { binds++; return v, nil }}
		sql, err := c.Expression(node)
		if err == nil || !strings.Contains(err.Error(), "text capability") || sql != "" || binds != 0 || len(c.Arguments) != 0 {
			t.Fatalf("operator %v: missing capability accepted: %q, %v", op, sql, err)
		}
		node.Text = true
		if _, err := c.Expression(node); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []Operator{Like, InsensitiveLike} {
		c := NewCompiler([]Column{{Name: "state"}})
		node := BinaryComparison{Left: FieldRef{Table: "items", Column: "state"}, Right: ParameterNode{Kind: codec.TypeText, Value: RetainLiteral("private")}, Operator: op}
		if sql, err := c.Expression(node); err == nil || sql != "" || len(c.Arguments) != 0 {
			t.Fatalf("binary pattern accepted: %q %v", sql, err)
		}
		node.Text = true
		if _, err := c.Expression(node); err != nil {
			t.Fatal(err)
		}
	}
}
