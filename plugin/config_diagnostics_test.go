package plugin_test

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/plugin"
)

type privateConfigError struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateConfigError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private configuration")
}
func (e *privateConfigError) Format(fmt.State, rune) { _ = e.Error() }

func TestLoadConfigBoundaryDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, message string
		cause         bool
	}{
		{"unknown", "unknown configuration field plugins.reports.missing in application", false},
		{"source", "configuration source needs a name", false},
		{"namespace", "configuration field is outside its declared namespace", false},
		{"manifest", "plugin version exceeds its declaration bound or is empty", false},
		{"override", "override references an undeclared setting", false},
		{"decode panic formatter", "cannot decode configuration field plugins.reports.label from application", true},
		{"decode goexit formatter", "cannot decode configuration field plugins.reports.label from application", true},
		{"validate", "configuration validation failed", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := &privateConfigError{exit: test.name == "decode goexit formatter"}
			declaration := plugin.Manifest{ID: "reports", Version: "1.0.0", Framework: "^0.1.0"}
			key := config.NewKey("plugins.reports.label", func(v *settings) *string { return &v.Label }, func(v string) (string, error) {
				if test.cause && test.name != "validate" {
					return "", cause
				}
				return v, nil
			})
			input := config.Inputs[settings]{}
			switch test.name {
			case "unknown":
				input.Files = []config.Values{{Name: "application", Data: map[string]string{"plugins.reports.missing": "private value"}}}
			case "source":
				input.Files = []config.Values{{Name: ""}}
			case "namespace":
				key = config.String("plugins.other.label", func(v *settings) *string { return &v.Label })
			case "manifest":
				declaration.Version = ""
			case "override":
				input.Overrides = []config.Override[settings]{config.String("plugins.other.label", func(v *settings) *string { return &v.Label }).Set("private value")}
			case "validate":
				input.Validate = func(settings) error { return cause }
			default:
				input.Files = []config.Values{{Name: "application", Data: map[string]string{key.Name(): "private value"}}}
			}
			schema, err := config.New(key)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = plugin.LoadConfig(declaration, schema, settings{}, input)
			var failure *fault.Error
			want := "load plugin configuration: invalid: " + test.message
			if err == nil || err.Error() != want || !errors.Is(err, fault.Invalid) || !errors.As(err, &failure) || failure.Message() != test.message || !errors.Is(err, failure) {
				t.Fatalf("configuration diagnostic: %v", err)
			}
			if test.cause {
				var private *privateConfigError
				if !errors.Is(err, cause) || !errors.As(err, &private) || private != cause {
					t.Fatal("configuration lost decoder/validator identity")
				}
			}
			*failure = *fault.Wrap(fault.Invalid, "private mutation", cause)
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.calls.Load() != 0 {
				t.Fatal("configuration exposed application formatting or mutable message")
			}
		})
	}
}
