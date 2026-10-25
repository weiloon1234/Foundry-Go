package cli_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
)

type privateCLIDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateCLIDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private CLI error")
}
func (e *privateCLIDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestCLIUsageSourceDiagnosticsCrossCallbacks(t *testing.T) {
	registry, err := cli.New()
	if err != nil {
		t.Fatal(err)
	}
	configure := func(*flag.FlagSet, *struct{}) {}
	for _, test := range []struct {
		name, message string
		owned         bool
		run           func() error
	}{
		{"missing command", "expected a command; use --help", true, func() error { _, err := registry.Parse(nil, io.Discard); return err }},
		{"unknown command", "unknown command; use --help", true, func() error { _, err := registry.Parse([]string{"absent"}, io.Discard); return err }},
		{"argument bound", "too many command arguments", true, func() error { _, err := registry.Parse(make([]string, cli.MaxArguments+1), io.Discard); return err }},
		{"argument text", "command arguments exceed their bounds or contain invalid text", true, func() error { _, err := registry.Parse([]string{"\x00"}, io.Discard); return err }},
		{"flags only", "command accepts flags only", true, func() error { _, err := cli.Flags(configure, nil)([]string{"extra"}, io.Discard); return err }},
		{"positional count", "expected 2 positional argument(s), got 0", true, func() error {
			_, err := cli.FlagsWithArgs(configure, cli.ExactArgs(2, func(*struct{}, []string) error { return nil }), nil)(nil, io.Discard)
			return err
		}},
		{"framework command", "framework command needs an identifier", true, func() error { return clidiag.Usage("framework command needs an identifier") }},
		{"owned validator", "framework command needs an identifier", true, func() error {
			_, err := cli.Flags(configure, func(struct{}) error { return clidiag.Usage("framework command needs an identifier") })(nil, io.Discard)
			return err
		}},
		{"public application usage", "", false, func() error { return cli.Usage("private application message") }},
	} {
		for _, exit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/goexit=%t", test.name, exit), func(t *testing.T) {
				original := test.run()
				var usage *cli.UsageError
				if !errors.As(original, &usage) {
					t.Fatal("missing usage classification")
				}
				cause := &privateCLIDiagnostic{exit: exit}
				*usage = *cli.InvalidArguments(cause).(*cli.UsageError)
				err := callback.Isolated("consumer", func() error { return original })
				want := "consumer failed"
				if test.owned {
					want = "consumer: " + test.message
				}
				var retained *cli.UsageError
				if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != usage || cli.Status(err) != cli.InvalidUsage {
					t.Fatal("usage diagnostic lost snapshot, identity or status")
				}
				var output bytes.Buffer
				if cli.Report(&output, err) != cli.InvalidUsage || output.String() != want+"\n" || fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want || cause.calls.Load() != 0 {
					t.Fatal("CLI containment formatted an application cause")
				}
			})
		}
	}
}

func TestCLIInvalidArgumentsRetainsSourceIdentity(t *testing.T) {
	for _, owned := range []bool{false, true} {
		cause := &privateCLIDiagnostic{}
		var source error = cause
		if owned {
			source = clidiag.Usage("framework validation failed")
		}
		wrapped := cli.InvalidArguments(source)
		err := callback.Isolated("consumer", func() error { return wrapped })
		want := "consumer failed"
		if owned {
			want = "consumer: framework validation failed"
		}
		var usage *cli.UsageError
		if err.Error() != want || !errors.Is(err, source) || !errors.Is(err, wrapped) || !errors.As(err, &usage) || cli.Status(err) != cli.InvalidUsage || cause.calls.Load() != 0 {
			t.Fatal("argument wrapper lost source diagnostic or formatted private text")
		}
	}
}
