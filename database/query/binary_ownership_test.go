package query

import (
	"bytes"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

type binaryRecord struct{}
type binaryInput []byte

func TestBinaryPredicatesOwnLazyBindings(t *testing.T) {
	f := NewBinaryField[binaryRecord]("binary_records", "body", codec.Bytes[binaryInput]())
	input := binaryInput{1, 2}
	predicates := []Predicate[binaryRecord]{f.Eq(input), f.Ne(input), f.In(input)}
	input[0] = 9
	for _, p := range predicates {
		comparison := p.expression.(comparison)
		bound, err := comparison.bind(comparison.values[0])
		if err != nil || !bytes.Equal(bound.([]byte), []byte{1, 2}) {
			t.Fatal("predicate did not capture binary data", err)
		}
	}
}

func TestBinaryAssignmentsAndMutatorsOwnReusableInputs(t *testing.T) {
	c := codec.Bytes[binaryInput]()
	for _, clonedInput := range []bool{false, true} {
		input := binaryInput{1, 2}
		a := Assign[binaryRecord]("binary_records", "body", c, input)
		if clonedInput {
			a = AssignClonedInput[binaryRecord]("binary_records", "body", c, input)
		}
		input[0] = 9
		values, err := ReadMutation(Change(a))
		if err != nil {
			t.Fatal(err)
		}
		first, err := MutationValue[binaryRecord, binaryInput](values, "binary_records", "body")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := first.Get()
		if data[0] != 1 {
			t.Fatal("assignment aliased input")
		}
		data[0] = 7
		second, err := MutationValue[binaryRecord, binaryInput](values, "binary_records", "body")
		data, _ = second.Get()
		if err != nil || data[0] != 1 {
			t.Fatal("mutation reader exposed owned input", err)
		}
		mutator := newFieldMutator("body", c, func(v binaryInput) (binaryInput, error) { v[0]++; return v, nil })
		for range 2 {
			result, err := mutator.apply(a.assignmentValue)
			if err != nil {
				t.Fatal(err)
			}
			bound, err := result.bind()
			if err != nil || !bytes.Equal(bound.([]byte), []byte{2, 2}) {
				t.Fatal("reused mutation changed its original input", err)
			}
		}
	}
}
