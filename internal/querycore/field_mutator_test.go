package querycore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
)

type privateMutatorError struct{ input string }

func (e *privateMutatorError) Error() string { return "invalid input: " + e.input }
func TestMutatorErrorsRetainCauseWithoutFormattingInput(t *testing.T) {
	const input = "private-mutator-value"
	cause := &privateMutatorError{input: input}
	transform := NewFieldMutator("email", codec.String[string](), func(v string) (string, error) { return "", cause })
	_, err := transform.Apply(CaptureInput(input, nil))
	if !errors.Is(err, cause) {
		t.Fatal("lost error identity")
	}
	var got *privateMutatorError
	if !errors.As(err, &got) || got != cause {
		t.Fatal("lost typed cause")
	}
	for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(text, input) || !strings.Contains(text, "email") {
			t.Fatal("unsafe mutator diagnostic")
		}
	}
}
