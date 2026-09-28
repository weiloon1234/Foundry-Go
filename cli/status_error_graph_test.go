package cli_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/cli"
)

type cyclicStatusError struct{ calls int }

func (*cyclicStatusError) Error() string { panic("status must not format errors") }
func (e *cyclicStatusError) Unwrap() error {
	e.calls++
	if e.calls > 512 {
		return nil
	}
	return e
}

type customUsageError struct{}

func (customUsageError) Error() string { panic("status must not format errors") }
func (customUsageError) As(target any) bool {
	if destination, ok := target.(**cli.UsageError); ok {
		*destination = cli.Usage("custom").(*cli.UsageError)
		return true
	}
	return false
}

func TestStatusBoundsCyclesAndKeepsTypedUsagePrecedence(t *testing.T) {
	cycle := &cyclicStatusError{}
	if got := cli.Status(cycle); got != cli.Failure || cycle.calls > 256 {
		t.Fatal("status did not bound the error graph", got, cycle.calls)
	}
	for _, test := range []struct {
		err  error
		want cli.ExitCode
	}{
		{customUsageError{}, cli.InvalidUsage},
		{errors.Join(cli.Usage("input"), context.Canceled), cli.Interrupted},
		{errors.Join(context.Canceled, context.DeadlineExceeded), cli.TimedOut},
	} {
		if got := cli.Status(test.err); got != test.want {
			t.Fatal("CLI status precedence changed", got, test.want)
		}
	}
}
