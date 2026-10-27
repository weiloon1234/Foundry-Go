package foundation_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

func TestConstructionBoundaryDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    fault.Code
		message string
	}{
		{"missing", fault.Missing, "construct service a: missing: service b is not registered"},
		{"cycle", fault.Cycle, "construct service a: construct service b: cycle: constructor cycle at service a"},
		{"type", fault.Invalid, "construct service a: invalid: service b was requested with a different type"},
		{"nil", fault.Invalid, "construct service a: invalid: constructor returned a nil service"},
		{"hostile panic", fault.Invalid, "construct service a failed"},
		{"hostile goexit", fault.Invalid, "construct service a failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, b := foundation.NewKey[*int]("a"), foundation.NewKey[*int]("b")
			cause := &privateRegistrationError{exit: test.name == "hostile goexit"}
			var original error
			var raw *fault.Error
			capture := func(err error) error {
				original = err
				if err != nil {
					errors.As(err, &raw)
				}
				return err
			}
			register := func(r *foundation.Registrar) error {
				if err := foundation.Factory(r, a, func(s foundation.Resolver) (*int, error) {
					switch test.name {
					case "nil":
						return nil, nil
					case "hostile panic", "hostile goexit":
						return nil, capture(fault.Wrap(fault.Invalid, "private constructor diagnostic", cause))
					case "type":
						_, err := foundation.Resolve(s, foundation.NewKey[string]("b"))
						return nil, capture(err)
					default:
						v, err := foundation.Resolve(s, b)
						return v, capture(err)
					}
				}); err != nil {
					return err
				}
				switch test.name {
				case "cycle":
					return foundation.Factory(r, b, func(s foundation.Resolver) (*int, error) { return foundation.Resolve(s, a) })
				case "type":
					return foundation.Provide(r, b, new(int))
				}
				return nil
			}
			_, err := foundation.NewBuilder().Register(foundation.Module{Name: "test", OnRegister: register}).Build(t.Context())
			var retained *fault.Error
			if err == nil || err.Error() != test.message || !errors.Is(err, test.code) || !errors.As(err, &retained) {
				t.Fatalf("construction diagnostic: %v", err)
			}
			if original != nil && (!errors.Is(err, original) || !errors.Is(err, raw) || retained != raw) {
				t.Fatal("construction lost error identity")
			}
			if strings.HasPrefix(test.name, "hostile") {
				var private *privateRegistrationError
				if !errors.Is(err, cause) || !errors.As(err, &private) || private != cause {
					t.Fatal("construction lost private cause")
				}
			}
			_ = fmt.Sprintf("%v %+v %#v", err, err, err)
			if cause.calls.Load() != 0 {
				t.Fatal("construction invoked application formatting")
			}
		})
	}
}
