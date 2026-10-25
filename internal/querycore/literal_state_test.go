package querycore

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

func TestLiteralStorageRedactsEveryASTLeaf(t *testing.T) {
	const secret = "literal-leaf-SECRET-726194"
	field := FieldRef{Table: "records", Column: "id"}
	for name, node := range map[string]any{
		"comparison":      Comparison{Operand: field, Operator: Equal, Values: RetainComparisonValues([]any{secret})},
		"row-comparison":  RowComparison{Operands: []ValueExpression{field, field}, Operator: Greater, Values: RetainRowValues([]driver.Value{secret, secret})},
		"parameter":       ParameterNode{Kind: codec.TypeText, Value: RetainLiteral(secret)},
		"schema-literal":  ParameterNode{Kind: codec.TypeText, Value: RetainLiteral(secret), Literal: true},
		"window-fallback": WindowNode{Kind: LagWindow, HasFallback: true, Fallback: RetainLiteral(secret)},
		"raw-argument":    NewRawFragment("?", []any{secret}),
		"raw-sql":         NewRawFragment("'"+secret+"'", nil),
	} {
		t.Run(name, func(t *testing.T) {
			for _, input := range []any{node, struct{ private any }{node}, struct{ nested any }{struct{ private any }{node}}} {
				for _, format := range []string{"%v", "%+v", "%#v"} {
					for _, text := range []string{fmt.Sprintf(format, input), fmt.Errorf("diagnostic: "+format, input).Error()} {
						if strings.Contains(text, secret) {
							t.Fatalf("%s exposed a retained literal", format)
						}
					}
				}
			}
		})
	}
	// Zero holders keep the old nil/empty semantics; retained SQL NULL does too.
	if LiteralValue(nil) != nil || LiteralValue(RetainLiteral(nil)) != nil || ComparisonValues(nil) != nil || RowValues(nil) != nil || OffsetValue(nil) != 0 {
		t.Fatal("zero literal semantics changed")
	}
}
