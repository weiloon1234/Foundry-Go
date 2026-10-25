package credential

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// Classification never authorizes replay. Keep confirmed contention distinct
// from internal failures without hiding a committed or unknown database outcome.
func operationFailureCode(err error) fault.Code {
	var conflict, overloaded, timeout, unsafe, complete bool
	failed := callback.Isolated("classify credential failure", func() error {
		complete = errorgraph.Walk(err, func(current error) bool {
			if errorgraph.Matches(current, database.CommitUnknown) || errorgraph.Matches(current, database.AfterCommitFailed) {
				unsafe = true
			}
			if errorgraph.Matches(current, context.DeadlineExceeded) {
				timeout = true
			}
			// Inspect this node only: As may expose an outcome without Unwrap.
			// Walk retains the shared traversal budget and isolation owns As.
			if failure, found := errorgraph.AsShallow[*database.Error](current); found {
				if failure == nil {
					unsafe = true
					return false
				}
				if failure.Outcome() == database.Unknown || failure.Outcome() == database.Committed || failure.Code() == database.CommitUnknown || failure.Code() == database.AfterCommitFailed {
					unsafe = true
				}
				if failure.Outcome() == database.RolledBack {
					switch failure.Code() {
					case database.LockNotAvailable, database.Deadlock, database.SerializationFailure:
						conflict = true
					}
				}
			}
			switch failure := current.(type) {
			case *fault.Error:
				if failure == nil {
					unsafe = true
					break
				}
				overloaded = overloaded || failure.Code() == fault.Overloaded
				timeout = timeout || failure.Code() == fault.Timeout
				unsafe = unsafe || failure.Code() == fault.Panicked
			}
			return true
		})
		return nil
	})
	switch {
	case failed != nil || !complete || unsafe:
		return fault.Internal
	case overloaded:
		return fault.Overloaded
	case conflict:
		return fault.Conflict
	case timeout:
		return fault.Timeout
	default:
		return fault.Internal
	}
}
