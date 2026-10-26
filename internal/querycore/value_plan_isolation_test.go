package querycore

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"reflect"
	"testing"
)

func TestCanonicalValuePlanningIsIsolated(t *testing.T) {
	input := `"$10", 'it''s $11', $123`
	parameter := ParameterNode{Kind: codec.TypeText, Value: RetainLiteral(input)}
	c := Compiler{Arguments: []any{int64(8)}}
	plan, err := c.PlanValue(parameter)
	if err != nil || plan.Sql != "CAST($1 AS text)" || !reflect.DeepEqual(plan.Arguments, []any{input}) || !reflect.DeepEqual(c.Arguments, []any{int64(8)}) {
		t.Fatal("planning changed existing bindings or SQL-looking text", plan, c.Arguments, err)
	}
}
