package toml_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type privateTOMLDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateTOMLDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private TOML diagnostic")
}

func (e *privateTOMLDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateTOMLDiagnostic) Unwrap() error            { return fault.Conflict }

func TestLoadFileRetainsSourceDiagnosticThroughConstruction(t *testing.T) {
	type settings struct{ Port int }
	for _, stage := range []string{"parse", "limit", "decoder panic", "decoder goexit"} {
		t.Run(stage, func(t *testing.T) {
			cause := &privateTOMLDiagnostic{exit: stage == "decoder goexit"}
			port := config.Int("port", func(s *settings) *int { return &s.Port })
			private := stage == "decoder panic" || stage == "decoder goexit"
			if private {
				port = config.NewKey("port", func(s *settings) *int { return &s.Port }, func(string) (int, error) { return 0, cause })
			}
			schema, err := config.New(port)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "private-configuration-path.toml")
			contents := "port = 12\n"
			options := toml.Options{Name: "settings"}
			want := "construct service settings: invalid: cannot parse TOML configuration from settings"
			switch stage {
			case "parse":
				contents = "port = [private-configuration-value\n"
			case "limit":
				options.MaxBytes = 1
				want = "construct service settings: invalid: TOML configuration exceeds byte limit in settings"
			default:
				want = "construct service settings: invalid: cannot decode configuration field port from settings"
			}
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			var original error
			var source *fault.Error
			var parserCause error
			key := foundation.NewKey[settings]("settings")
			register := func(r *foundation.Registrar) error {
				return foundation.Factory(r, key, func(foundation.Resolver) (settings, error) {
					result, report, err := toml.LoadFile(path, schema, settings{Port: 99}, config.Inputs[settings]{}, options)
					original = err
					if err != nil {
						errors.As(err, &source)
						if stage == "parse" && source != nil {
							var parseFault *fault.Error
							if errors.As(errors.Unwrap(source), &parseFault) {
								parserCause = errors.Unwrap(parseFault)
							}
						}
						if result != (settings{}) || len(report.Entries()) != 0 {
							t.Error("failed file loading exposed partial configuration")
						}
					}
					return result, err
				})
			}
			_, err = foundation.NewBuilder().Register(foundation.Module{Name: "configuration", OnRegister: register}).Build(t.Context())
			var retained *fault.Error
			if err == nil || original == nil || !errors.Is(err, original) || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != source {
				t.Fatal("file loading lost its diagnostic, classification or source identity")
			}
			if stage == "parse" && (parserCause == nil || !errors.Is(err, parserCause)) {
				t.Fatal("file loading lost its parser cause")
			}
			if private {
				var retainedCause *privateTOMLDiagnostic
				if !errors.Is(err, cause) || !errors.As(err, &retainedCause) || retainedCause != cause {
					t.Fatal("file loading lost its private decoder cause")
				}
			}
			*source = *fault.Wrap(fault.Invalid, "private replacement", cause)
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, err); got != want {
					t.Fatalf("file loading diagnostic = %q, want %q", got, want)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("file loading formatted an application cause")
			}
		})
	}
}
