package querycore

import (
	"errors"
	"fmt"
	"testing"
)

type privateMapKey struct{ cause *privateCompilerError }

func (k privateMapKey) MarshalText() ([]byte, error) { return nil, k.cause }
func (*privateMapKey) UnmarshalText([]byte) error    { return nil }

func TestJSONMapKeyPreservesPrivateMarshalerCause(t *testing.T) {
	cause := &privateCompilerError{}
	_, err := JSONMapKey(privateMapKey{cause})
	var retained *privateCompilerError
	if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("JSON path map key lost cause")
	}
	if got := fmt.Sprintf("%v", err); got != "invalid: invalid JSON map key" {
		t.Fatal("unsafe JSON path diagnostic")
	}
}
