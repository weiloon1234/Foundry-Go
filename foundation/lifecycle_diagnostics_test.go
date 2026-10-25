package foundation_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestLifecycleDiagnosticsUseConstructionSnapshots(t *testing.T) {
	for _, phase := range []string{"boot", "shutdown"} {
		for _, owned := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owned=%t/exit=%t", phase, owned, exit), func(t *testing.T) {
					cause := &privateRegistrationError{exit: exit}
					var failure error = fault.Wrap(fault.Invalid, "private application fault", cause)
					if owned {
						failure = safeerror.WrapFault(fault.Invalid, "owned lifecycle diagnostic", cause)
					}
					var raw *fault.Error
					errors.As(failure, &raw)
					*raw = *fault.Wrap(fault.Invalid, "private mutated fault", cause)
					var output lockedBuffer
					app, err := foundation.NewBuilder(foundation.WithLogger(slog.New(slog.NewJSONHandler(&output, nil)))).Register(foundation.Module{Name: "diagnostic", OnBoot: func(_ context.Context, r *foundation.Runtime) error {
						if phase == "boot" {
							return failure
						}
						return r.OnShutdown("diagnostic", func(context.Context) error { return failure })
					}}).Build(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					err = app.Start(t.Context())
					if phase == "shutdown" {
						if err != nil {
							t.Fatal(err)
						}
						err = stop(t, app)
					}
					if err == nil || !errors.Is(err, failure) || !errors.Is(err, cause) || !errors.Is(err, fault.Invalid) {
						t.Fatal("lifecycle identity lost")
					}
					text := output.String()
					if strings.Contains(text, "private mutated fault") || strings.Contains(text, "private application fault") || strings.Contains(err.Error(), "private") || cause.calls.Load() != 0 {
						t.Fatal("lifecycle exposed application text")
					}
					if owned && (!strings.Contains(text, "owned lifecycle diagnostic") || !strings.Contains(err.Error(), "owned lifecycle diagnostic")) {
						t.Fatal("owned lifecycle diagnostic lost")
					}
				})
			}
		}
	}
}
