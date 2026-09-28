package query

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestMutationValuesPreserveTypedInputsWithoutBinding(t *testing.T) {
	calls := 0
	c := codec.String[string]().Validated(func(string) error { calls++; return errors.New("must not bind") })
	name := Assign[mutatorRecord]("records", "name", c, "private-input")
	note := Assign[mutatorRecord]("records", "note", codec.Nullable(codec.String[string]()), value.Null[string]())
	mutation := Change(name, note)
	inputs, err := ReadMutation(mutation)
	if err != nil {
		t.Fatal(err)
	}
	text, err := MutationValue[mutatorRecord, string](inputs, "records", "name")
	if v, ok := text.Get(); err != nil || !ok || v != "private-input" {
		t.Fatal("input lost its exact value")
	}
	null, err := MutationValue[mutatorRecord, value.Nullable[string]](inputs, "records", "note")
	if v, ok := null.Get(); err != nil || !ok || !v.IsNull() {
		t.Fatal("NULL changed into omission")
	}
	missing, err := MutationValue[mutatorRecord, int](inputs, "records", "absent")
	if err != nil || missing.IsSet() || inputs.Has("other", "name") || !inputs.Has("records", "name") {
		t.Fatal("assignment presence crossed a field boundary")
	}
	if result, err := MutationValue[mutatorRecord, int](inputs, "records", "name"); !errors.Is(err, fault.Invalid) || result.IsSet() {
		t.Fatal("input was coerced into another type")
	}
	if calls != 0 {
		t.Fatal("reading mutation inputs invoked a codec")
	}
	mutation.assignments[0] = note
	if !inputs.Has("records", "name") {
		t.Fatal("index retained caller's assignment slice")
	}
	for _, diagnostic := range []string{fmt.Sprint(inputs), fmt.Sprintf("%#v", inputs), fmt.Sprintf("%+v", inputs)} {
		if strings.Contains(diagnostic, "private-input") {
			t.Fatal("input leaked into diagnostics")
		}
	}
}

func TestMutationValuesRejectMalformedAssignments(t *testing.T) {
	a := Assign[mutatorRecord]("records", "name", codec.String[string](), "secret")
	for _, mutation := range []Mutation[mutatorRecord]{Change(a, a), Change(Assignment[mutatorRecord]{}), Change(Assign[mutatorRecord]("invalid table", "name", codec.String[string](), "x")), Change(make([]Assignment[mutatorRecord], MaxExpressionNodes+1)...)} {
		inputs, err := ReadMutation(mutation)
		if !errors.Is(err, fault.Invalid) || len(inputs.values) != 0 {
			t.Fatalf("malformed mutation returned partial values: %v", err)
		}
	}
}
