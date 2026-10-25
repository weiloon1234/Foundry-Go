package querycore

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateCompilerError struct{}

func (*privateCompilerError) Error() string          { panic("private compiler error") }
func (*privateCompilerError) Format(fmt.State, rune) { panic("private compiler formatter") }

func TestComparisonRedactsBindingCallbackFailure(t *testing.T) {
	cause := &privateCompilerError{}
	c := NewCompiler([]Column{{Name: "name"}})
	_, err := c.Expression(Comparison{Operand: FieldRef{Table: "records", Column: "name"}, Operator: Equal, Kind: codec.TypeText, Values: RetainComparisonValues([]any{"private"}), Bind: func(any) (driver.Value, error) { return nil, cause }})
	var retained *privateCompilerError
	if !errors.Is(err, cause) || errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("compiler lost binding cause")
	}
	if got := fmt.Sprintf("%v", err); got != "query comparison binding failed" {
		t.Fatal("unsafe compiler diagnostic")
	}
}
