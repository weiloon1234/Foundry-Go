package observability

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

// OutcomeFor inspects errors inside owned isolation because custom Is/Unwrap
// methods can panic or call Goexit. Traversal is bounded: cyclic or excessively
// large/deep graphs report Failed. Methods must return and Is must perform a
// shallow comparison, as required by errors.Is. No error text is formatted.
func OutcomeFor(err error) Outcome {
	if err == nil {
		return Succeeded
	}
	outcome := Failed
	inspection := callback.Isolated("classify observed outcome", func() error {
		outcome = inspectOutcome(err)
		return nil
	})
	if inspection != nil {
		return Panicked
	}
	return outcome
}

func inspectOutcome(err error) Outcome {
	// Preserve classification precedence across the whole error tree, including
	// joined errors, without repeatedly traversing each chain with errors.Is.
	targets := [...]struct {
		err     error
		outcome Outcome
	}{
		{maintenance.ErrMaintenance, Rejected},
		{maintenance.ErrDraining, Rejected},
		{fault.Panicked, Panicked},
		{context.DeadlineExceeded, TimedOut},
		{context.Canceled, Cancelled},
	}
	best := len(targets)
	complete := errorgraph.Walk(err, func(current error) bool {
		for i, target := range targets[:best] {
			if errorgraph.Matches(current, target.err) {
				best = i
				break
			}
		}
		return best > 1
	})
	if !complete {
		return Failed
	}
	if best == len(targets) {
		return Failed
	}
	return targets[best].outcome
}
