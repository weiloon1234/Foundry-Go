package querycore

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestComparisonBatchFailsClosedAfterEarlierSuccess(t *testing.T) {
	for _, mode := range []string{"error", "panic", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &privateCompilerError{}
			var active atomic.Int32
			for range 20 {
				calls := 0
				node := Comparison{Operand: FieldRef{Table: "records", Column: "name"}, Operator: In, Kind: codec.TypeText, Values: RetainComparisonValues([]any{"first", "private", "last"}), Bind: func(v any) (driver.Value, error) {
					active.Add(1)
					defer active.Add(-1)
					calls++
					if calls == 2 {
						switch mode {
						case "error":
							return "partial", cause
						case "exit":
							runtime.Goexit()
						default:
							panic("private value")
						}
					}
					return v.(string), nil
				}}
				compiler := NewCompiler([]Column{{Name: "name"}})
				sql, err := compiler.Expression(node)
				if err == nil || sql != "" || len(compiler.Arguments) != 0 || calls != 2 || active.Load() != 0 {
					t.Fatal("partial batch published or callback ownership released early")
				}
				if errors.Is(err, fault.Panicked) != (mode != "error") || errors.Is(err, cause) != (mode == "error") || errors.Is(err, fault.Invalid) {
					t.Fatal("batch classification or identity changed")
				}
				want := map[string]string{"error": "query comparison binding failed", "panic": "panicked: query comparison binding panicked", "exit": "panicked: query comparison binding exited without returning"}[mode]
				if fmt.Sprintf("%v", err) != want {
					t.Fatal("batch diagnostic changed")
				}
			}
		})
	}
}

func TestRetainedComparisonRedactsAndKeepsIndependentValues(t *testing.T) {
	for _, count := range []int{0, 1, 3} {
		node := Comparison{Operand: FieldRef{Table: "records", Column: "name"}, Operator: In, Kind: codec.TypeText}
		first, values := RetainComparison(node, count)
		for i := range values {
			values[i] = "private retained literal"
		}
		second, other := RetainComparison(node, count)
		for i := range other {
			other[i] = "other"
		}
		for _, state := range []*PredicateState{first, second} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				text := fmt.Sprintf(format, struct{ hidden *PredicateState }{state})
				if strings.Contains(text, "private retained literal") {
					t.Fatal("literal escaped opaque state")
				}
			}
		}
		if count > 0 && ComparisonValues(PredicateNode(first).(Comparison).Values)[0] != "private retained literal" {
			t.Fatal("retained value changed")
		}
	}
}

func TestComparisonBatchPreservesEarlierValidationFailure(t *testing.T) {
	calls := 0
	node := Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: In, Values: RetainComparisonValues([]any{1, 2}), Bind: func(any) (driver.Value, error) {
		calls++
		if calls > 1 {
			panic("later callback must not execute")
		}
		return nil, nil
	}}
	c := NewCompiler([]Column{{Name: "id"}})
	_, err := c.Expression(node)
	if !errors.Is(err, fault.Invalid) || errors.Is(err, fault.Panicked) || calls != 1 || err.Error() != "invalid: NULL comparison requires IsNull or IsNotNull" {
		t.Fatal("earlier validation failure changed")
	}
}

func TestComparisonOverrideCannotInheritCodecCapability(t *testing.T) {
	for _, exit := range []bool{false, true} {
		node := CodecComparison(Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: Equal, Values: RetainComparisonValues([]any{int64(1)})}, func(v any) (driver.Value, error) { return v, nil })
		node.Bind = func(any) (driver.Value, error) {
			if exit {
				runtime.Goexit()
			}
			panic("private overridden binding")
		}
		c := NewCompiler([]Column{{Name: "id"}})
		sql, err := c.Expression(node)
		if !errors.Is(err, fault.Panicked) || sql != "" || len(c.Arguments) != 0 {
			t.Fatal("override inherited codec capability")
		}
	}
}
