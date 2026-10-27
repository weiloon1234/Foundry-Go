package foundation_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
)

type privateRegistrationError struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateRegistrationError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private registration diagnostic")
}
func (e *privateRegistrationError) Format(fmt.State, rune) { _ = e.Error() }

func TestRegistrationDiagnosticsDoNotExposeProviderErrors(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			cause := &privateRegistrationError{exit: exit}
			register := func(*foundation.Registrar) error { return cause }
			builder := foundation.NewBuilder()
			if override {
				builder.OverrideContributions("application", register)
			} else {
				module := pluginModule("private")
				module.OnRegister = register
				builder.RegisterPlugin(module)
			}
			_, err := builder.Build(t.Context())
			var retained *privateRegistrationError
			if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
				t.Fatal("registration lost application error identity")
			}
			if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, "diagnostic") || cause.calls.Load() != 0 {
				t.Fatal("registration formatted an application error")
			}
		}
	}
	module := pluginModule("fault")
	cause := fault.New(fault.Invalid, "private registration diagnostic")
	module.OnRegister = func(*foundation.Registrar) error { return cause }
	_, err := foundation.NewBuilder().RegisterPlugin(module).Build(t.Context())
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
		t.Fatal("registration exposed an application-created fault")
	}
}

func TestOverrideKeepsRegistryDiagnosticAndFault(t *testing.T) {
	key := foundation.NewKey[string]("missing.service")
	_, err := foundation.NewBuilder().OverrideContributions("application", func(r *foundation.Registrar) error {
		return foundation.Provide(r, key, "value")
	}).Build(t.Context())
	var failure *fault.Error
	if !errors.Is(err, fault.Missing) || !errors.As(err, &failure) || failure.Message() != "cannot override missing service missing.service" || !strings.Contains(err.Error(), failure.Message()) {
		t.Fatal("override lost its registry diagnostic", err)
	}
}

func TestKernelRegistrationKeepsDuplicateOwners(t *testing.T) {
	register := func(r *foundation.Registrar) error {
		return r.Kernel(foundation.HTTP, func(*foundation.Runtime) (foundation.Kernel, error) {
			t.Fatal("duplicate registration constructed a kernel")
			return nil, nil
		})
	}
	_, err := foundation.NewBuilder().Register(
		foundation.Module{Name: "first", OnRegister: register},
		foundation.Module{Name: "second", OnRegister: register},
	).Build(t.Context())
	var failure *fault.Error
	if !errors.Is(err, fault.Duplicate) || !errors.As(err, &failure) || !strings.Contains(err.Error(), "first and second") || !strings.Contains(err.Error(), failure.Message()) {
		t.Fatal("kernel registration lost duplicate owners", err)
	}
}

func TestRegistrationDiagnosticSnapshotSurvivesCauseMutation(t *testing.T) {
	for _, exit := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprintf("override=%t/goexit=%t", override, exit), func(t *testing.T) {
				key := foundation.NewKey[string]("registered.service")
				cause := &privateRegistrationError{exit: exit}
				var marked error
				var original *fault.Error
				code := fault.Duplicate
				want := "duplicate: service registered.service belongs to both plugin:first and plugin:mutated"
				operation := "register provider plugin:mutated"
				if override {
					code = fault.Missing
					want = "missing: cannot override missing service registered.service"
					operation = "override contributions application"
				}
				register := func(r *foundation.Registrar) error {
					marked = foundation.Provide(r, key, "value")
					if !errors.As(marked, &original) {
						t.Error("registry did not return its typed fault")
						return marked
					}
					*original = *fault.Wrap(code, "private application text", cause)
					return marked
				}
				builder := foundation.NewBuilder()
				if override {
					builder.OverrideContributions("application", register)
				} else {
					first := pluginModule("first")
					first.OnRegister = func(r *foundation.Registrar) error { return foundation.Provide(r, key, "first") }
					mutated := pluginModule("mutated")
					mutated.OnRegister = register
					builder.RegisterPlugin(first, mutated)
				}
				_, err := builder.Build(t.Context())
				var retained *fault.Error
				var retainedCause *privateRegistrationError
				if !errors.Is(err, code) || !errors.Is(err, marked) || !errors.Is(err, original) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != original || !errors.As(err, &retainedCause) || retainedCause != cause {
					t.Fatal("mutated registration cause lost classification or identity")
				}
				if marked.Error() != want || err.Error() != operation+": "+want {
					t.Fatal("mutation changed the owned registry diagnostic")
				}
				if text := fmt.Sprintf("%v %+v %#v %v %+v %#v", marked, marked, marked, err, err, err); strings.Contains(text, "private") || cause.calls.Load() != 0 {
					t.Fatal("mutated registration cause reached formatting")
				}
			})
		}
	}
}
