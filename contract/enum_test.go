package contract

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type State string
type NumberState int16
type FailingState string

func (v FailingState) MarshalJSON() ([]byte, error) {
	if v == "exit" {
		runtime.Goexit()
	}
	panic("private enum panic")
}

func TestEnumContractReusesTypedDescriptor(t *testing.T) {
	descriptor := enum.Describe("sample.test/domain", "State", enum.Case[State]{Name: "Active", Value: "active"}, enum.Case[State]{Name: "Paused", Value: "paused"})
	schema := compileForTest(t, Schema{Root: "state", Types: []Type{EnumType("state", descriptor)}})
	issues, err := schema.check(context.Background(), "active", shapeLimits{steps: 10, issues: 10})
	if err != nil || len(issues) != 0 {
		t.Fatalf("declared enum rejected: %v %v", issues, err)
	}
	issues, err = schema.check(context.Background(), "other", shapeLimits{steps: 10, issues: 10})
	if err != nil || !reflect.DeepEqual(issues, []Issue{{Path: "", Code: ValueIssue}}) {
		t.Fatalf("undeclared enum accepted: %v %v", issues, err)
	}
	if schema.snapshot().Types[0].enumCases != nil {
		t.Fatal("export retained executable enum metadata")
	}
	integer := EnumType("number", enum.Describe("sample.test/domain", "NumberState", enum.Case[NumberState]{Name: "Minus", Value: -5}))
	if integer.Kind != IntegerKind || integer.Bits != 16 || !integer.Signed {
		t.Fatal("enum lost concrete numeric width")
	}
	compileForTest(t, Schema{Root: "number", Types: []Type{integer}})
}

func TestEnumContractContainsInvalidMetadataAndCodecFailures(t *testing.T) {
	for _, descriptor := range []enum.Descriptor[FailingState]{
		enum.Describe("sample.test/domain", "FailingState", enum.Case[FailingState]{Name: "Panic", Value: "panic"}),
		enum.Describe("sample.test/domain", "FailingState", enum.Case[FailingState]{Name: "Exit", Value: "exit"}),
	} {
		schema, err := compileSchema(Schema{Root: "state", Types: []Type{EnumType("state", descriptor)}})
		if schema != nil || !errors.Is(err, fault.Invalid) || !errors.Is(err, fault.Panicked) {
			t.Fatalf("enum codec failure escaped: %v", err)
		}
	}
	schema, err := compileSchema(Schema{Root: "state", Types: []Type{EnumType("state", enum.Descriptor[State]{})}})
	if schema != nil || !errors.Is(err, fault.Invalid) {
		t.Fatalf("invalid enum accepted: %v", err)
	}
}
