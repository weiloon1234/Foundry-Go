package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type ExitCode int

const (
	Success      ExitCode = 0
	Failure      ExitCode = 1
	InvalidUsage ExitCode = 2
	TimedOut     ExitCode = 124
	Interrupted  ExitCode = 130
)

// Status treats only a clean help result as success. A joined help/write error
// remains a failure. Classifying a custom error is isolated from panic/Goexit;
// cyclic or excessively large/deep error graphs return Failure. Custom error
// methods must return and perform shallow Is/As comparisons.
func Status(err error) ExitCode {
	if err == nil || err == flag.ErrHelp {
		return Success
	}
	code := Failure
	if inspect := callback.Isolated("classify CLI result", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			if errorgraph.Matches(current, context.DeadlineExceeded) {
				code = TimedOut
				return false
			}
			if errorgraph.Matches(current, context.Canceled) {
				code = Interrupted
			}
			if code == Failure {
				if _, ok := errorgraph.AsShallow[*UsageError](current); ok {
					code = InvalidUsage
				}
			}
			return true
		})
		if !complete {
			code = Failure
		}
		return nil
	}); inspect != nil {
		return Failure
	}
	return code
}

// Report writes an execution error once and returns its process exit code. The
// application's main function owns os.Exit and signal handling. A write failure
// or a misbehaving custom Error method is itself a failed report.
func Report(output io.Writer, err error) ExitCode {
	code := Status(err)
	if code == Success {
		return code
	}
	if output == nil {
		return Failure
	}
	if report := callback.Isolated("report CLI result", func() error { _, writeErr := fmt.Fprintln(output, err.Error()); return writeErr }); report != nil {
		return Failure
	}
	return code
}
