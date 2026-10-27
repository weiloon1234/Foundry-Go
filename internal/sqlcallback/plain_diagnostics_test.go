package sqlcallback

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestPointerLimitDiagnosticsRemainClassificationNeutral(t *testing.T) {
	for _, test := range []struct {
		name, message string
		run           func(cyclicArgument) error
	}{
		{"argument", "SQL argument pointer depth exceeded", func(cycle cyclicArgument) error {
			_, err := driver.DefaultParameterConverter.ConvertValue(Argument(cycle))
			return err
		}},
		{"destination", "scan SQL value: SQL destination pointer depth exceeded", func(cycle cyclicArgument) error {
			return Destinations([]any{&cycle})[0].(sql.Scanner).Scan(int64(1))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cycle cyclicArgument
			cycle = cyclicArgument(&cycle)
			original := test.run(cycle)
			if original == nil {
				t.Fatal("pointer limit accepted a cycle")
			}
			native := fmt.Errorf("native SQL conversion: %w", original)
			err := callback.Isolated("consume SQL result", func() error {
				return Retain(native, safeerror.Reveal("SQL conversion failed", native))
			})
			want := "consume SQL result: " + test.message
			var classified *fault.Error
			if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, native) || errors.As(err, &classified) {
				t.Fatal("pointer-limit diagnostic lost identity or acquired classification")
			}
			if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want {
				t.Fatal("pointer-limit formatting lost the source diagnostic")
			}
		})
	}
}

type privateDriverDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateDriverDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private driver diagnostic")
}
func (e *privateDriverDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestDriverNativeWrapperRetainsOnlyCapturedDiagnostics(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, retry := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("owned=%t/retry=%t/goexit=%t", owned, retry, exit), func(t *testing.T) {
					private := &privateDriverDiagnostic{exit: exit}
					var source error = private
					if owned {
						source = safeerror.Join("private callback failed", safeerror.New("PostgreSQL registration failed"), private)
					}
					sealed := safeerror.Seal("SQL callback failed", source)
					native := fmt.Errorf("native SQL callback: %w", sealed)
					var foreign error = native
					if retry {
						foreign = errors.Join(native, driver.ErrBadConn)
					}
					returned := call("connect SQL driver", func() error { return foreign })
					if errors.Is(returned, private) || errors.Is(returned, driver.ErrBadConn) != retry {
						t.Fatal("driver capsule exposed its cause or changed retry control")
					}
					err := safeerror.Reveal("driver failed", returned)
					want := "driver failed"
					if owned {
						want = "PostgreSQL registration failed\nprivate callback failed"
					}
					var retained *privateDriverDiagnostic
					var classified *fault.Error
					if err == nil || err.Error() != want || !errors.Is(err, foreign) || !errors.Is(err, source) || !errors.Is(err, private) || !errors.As(err, &retained) || retained != private || errors.As(err, &classified) {
						t.Fatal("driver native wrapper lost diagnostic, identity or classification neutrality")
					}
					if fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || private.calls.Load() != 0 {
						t.Fatal("driver boundary invoked application formatting")
					}
				})
			}
		}
	}
}
