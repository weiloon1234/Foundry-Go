package observability_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type cyclingOutcomeError struct{ calls int }

func (*cyclingOutcomeError) Error() string { panic("error text must not be inspected") }
func (e *cyclingOutcomeError) Unwrap() error {
	e.calls++
	// A finite escape keeps a regression on the old implementation from hanging
	// the suite; the assertion proves classification stops before this escape.
	if e.calls > 512 {
		return nil
	}
	return e
}

type joinedOutcomeError struct{ children []error }

func (joinedOutcomeError) Error() string     { panic("error text must not be inspected") }
func (e joinedOutcomeError) Unwrap() []error { return e.children }

type equivalentOutcomeError struct{ target error }

func (equivalentOutcomeError) Error() string          { panic("error text must not be inspected") }
func (e equivalentOutcomeError) Is(target error) bool { return target == e.target }

func TestOutcomeClassificationBoundsCyclesAndReleasesObservedWork(t *testing.T) {
	for _, joined := range []bool{false, true} {
		t.Run(fmt.Sprint("joined=", joined), func(t *testing.T) {
			cycle := &cyclingOutcomeError{}
			var failure error = cycle
			if joined {
				failure = joinedOutcomeError{[]error{errors.New("failure"), cycle}}
			}
			recorder, err := observability.New(observability.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			ctx := observability.WithContext(t.Context(), recorder)
			got := observability.Observe(ctx, observability.Operation{Kind: observability.Resource, Name: "cyclic.failure"}, func(context.Context) error { return failure })
			if got == nil || cycle.calls > 256 {
				t.Fatal("classification did not bound the error chain", cycle.calls)
			}
			snapshot := recorder.Snapshot()
			if snapshot.Active != 0 || snapshot.Completed != 1 || snapshot.Failures != 1 || len(snapshot.Recent) != 1 || snapshot.Recent[0].Result.Outcome != observability.Failed {
				t.Fatal("classification retained span ownership or lost its failure", snapshot)
			}
			if err := recorder.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOutcomeClassificationKeepsJoinedPrecedenceAndCustomMatching(t *testing.T) {
	cases := []struct {
		err  error
		want observability.Outcome
	}{
		{errors.Join(context.Canceled, context.DeadlineExceeded), observability.TimedOut},
		{errors.Join(context.DeadlineExceeded, fault.New(fault.Panicked, "callback failed")), observability.Panicked},
		{errors.Join(fault.Panicked, maintenance.ErrDraining), observability.Rejected},
		{errors.Join(context.Canceled, maintenance.ErrMaintenance), observability.Rejected},
		{equivalentOutcomeError{context.Canceled}, observability.Cancelled},
		{joinedOutcomeError{[]error{nil, context.Canceled}}, observability.Cancelled},
		{joinedOutcomeError{make([]error, 257)}, observability.Failed},
	}
	for _, test := range cases {
		if got := observability.OutcomeFor(test.err); got != test.want {
			t.Fatalf("outcome = %s, want %s", got, test.want)
		}
	}
}
