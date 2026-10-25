package querycore

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestRetainedInputPreservesCloneAndPreflight(t *testing.T) {
	clones := 0
	input := []byte("private-input")
	assigned := CaptureInput(input, func(v []byte) []byte { clones++; return append([]byte(nil), v...) })
	input[0] = '!'
	if clones != 1 || string(StoredInput(assigned.Value).([]byte)) != "private-input" {
		t.Fatal("capture ownership changed")
	}
	snapshot := assigned.InputValue().([]byte)
	snapshot[0] = '!'
	if clones != 2 || string(StoredInput(assigned.Value).([]byte)) != "private-input" {
		t.Fatal("input snapshot ownership changed")
	}
	fields := []FieldMutator{NewFieldMutator("name", codec.String[string](), func(s string) (string, error) { t.Fatal("invalid input reached mutator"); return s, nil })}
	_, err := MutateAssignments(t.Context(), []MutationAssignment{{Field: FieldRef{Column: "name"}, AssignmentValue: assigned}}, fields)
	if !errors.Is(err, fault.Invalid) || clones != 2 {
		t.Fatal("type preflight changed classification or invoked clone")
	}
	fields = []FieldMutator{NewFieldMutator("name", codec.String[string](), func(v []byte) (string, error) { return string(v), nil })}
	result, err := MutateAssignments(t.Context(), []MutationAssignment{{Field: FieldRef{Column: "name"}, AssignmentValue: assigned}}, fields)
	if err != nil || clones != 3 {
		t.Fatal("transform changed input clone count")
	}
	bound, err := result[0].Bind()
	if err != nil || bound != "private-input" || result[0].InputValue() != "private-input" {
		t.Fatal("stored input or binding changed")
	}
	if StoredInput(nil) != nil || (AssignmentValue{}).InputValue() != nil || !reflect.DeepEqual(StatementValue(nil), Statement{}) {
		t.Fatal("zero retained state changed")
	}
}
