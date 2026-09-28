package foundation

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/fault"
	"runtime"
	"testing"
)

type cyclicCancellation struct{}

func (cyclicCancellation) Error() string { return "cyclic" }
func (cyclicCancellation) Unwrap() error { return cyclicCancellation{} }

type exitingCancellation struct{}

func (exitingCancellation) Error() string { return "exit" }
func (exitingCancellation) Unwrap() error { runtime.Goexit(); return nil }
func TestCancellationClassificationRetainsEveryRealFailure(t *testing.T) {
	failure := errors.New("real failure")
	for _, test := range []struct {
		err  error
		only bool
	}{{nil, false}, {context.Canceled, true}, {fmt.Errorf("wrapped: %w", errors.Join(context.Canceled)), true}, {errors.Join(context.Canceled, context.DeadlineExceeded), true}, {errors.Join(context.Canceled, failure), false}, {errors.Join(failure, context.Canceled), false}, {fault.Wrap(fault.Timeout, "shutdown grace expired", context.DeadlineExceeded), false},
		{errors.Join(context.Canceled, fault.Wrap(fault.Timeout, "shutdown grace expired", context.DeadlineExceeded)), false},
		{cyclicCancellation{}, false}, {exitingCancellation{}, false}} {
		if got := cancellationOnly(test.err); got != test.only {
			t.Fatalf("classification %T = %v want %v", test.err, got, test.only)
		}
	}
}
