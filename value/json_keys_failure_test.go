package value_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

type roundTripKey string

var roundTripCause = &privateKeyError{}

func (k *roundTripKey) UnmarshalText(text []byte) error { *k = roundTripKey(text); return nil }
func (k roundTripKey) MarshalText() ([]byte, error) {
	switch k {
	case "panic":
		panic("private key panic")
	case "goexit":
		runtime.Goexit()
	case "error":
		return nil, roundTripCause
	}
	return []byte(k), nil
}

func TestJSONKeyRoundTripFailureClassification(t *testing.T) {
	for _, name := range []string{"panic", "goexit", "error"} {
		t.Run(name, func(t *testing.T) {
			err := value.CheckJSONKeys[roundTripKey](t.Context(), []string{name}, value.JSONKeyLimits{Bytes: 64, Keys: 1})
			var failure *fault.Error
			ok := errors.As(err, &failure)
			want := fault.Internal
			if name == "error" {
				want = fault.Invalid
			}
			if !ok || failure.Code() != want {
				t.Fatal("key round-trip classification changed", err)
			}
			if name == "error" {
				var retained *privateKeyError
				if !errors.Is(err, roundTripCause) || !errors.As(err, &retained) || retained != roundTripCause {
					t.Fatal("key round-trip cause lost")
				}
			}
			if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, "private") || roundTripCause.formatted.Load() != 0 {
				t.Fatal("key round-trip error was formatted")
			}
		})
	}
}
