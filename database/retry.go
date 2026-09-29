package database

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// RetryPolicy bounds automatic re-execution of a whole transaction. Attempts
// counts every execution, including the first. Waits between attempts use full
// jitter over an exponentially growing delay capped at MaxDelay.
type RetryPolicy struct {
	Attempts     int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// DefaultRetryPolicy allows three attempts with short jittered waits.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Attempts: 3, InitialDelay: 20 * time.Millisecond, MaxDelay: 500 * time.Millisecond}
}

// MaxRetryAttempts bounds one Retry call.
const MaxRetryAttempts = 10

func (p RetryPolicy) Validate() error {
	if p.Attempts < 1 || p.Attempts > MaxRetryAttempts || p.InitialDelay <= 0 || p.MaxDelay < p.InitialDelay {
		return fault.New(fault.Invalid, "invalid transaction retry policy")
	}
	return nil
}

// Retry runs fn in a transaction of transactor and runs the whole transaction
// again only when the previous attempt was a confirmed rollback caused by a
// serialization failure or deadlock. Every other failure is returned at once:
// an unknown commit outcome is never retried, because the attempt may have
// committed. The callback must therefore be safe to run more than once;
// process-local effects belong in Tx.AfterCommit, which runs only after the
// attempt that commits. A nested *Tx is rejected: its savepoint cannot recover
// a serialization failure of the enclosing transaction. The last failure is
// returned when attempts are exhausted or ctx ends while waiting.
func Retry(ctx context.Context, transactor Transactor, policy RetryPolicy, fn func(*Tx) error, options ...TxOptions) error {
	if ctx == nil || transactor == nil || fn == nil {
		return fault.New(fault.Invalid, "transaction retry requires a context, transactor and callback")
	}
	if _, nested := transactor.(*Tx); nested {
		return fault.New(fault.Invalid, "transaction retry requires an outer transaction owner, not a savepoint")
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	delay := policy.InitialDelay
	for attempt := 1; ; attempt++ {
		err := transactor.Transaction(ctx, fn, options...)
		if err == nil || attempt == policy.Attempts || ctx.Err() != nil || !retryableRollback(err) {
			return err
		}
		timer := time.NewTimer(rand.N(delay) + 1)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		delay += min(delay, policy.MaxDelay-delay)
	}
}

// retryableRollback uses bounded inspection of the framework classification.
// An incomplete or failed inspection is never retryable.
func retryableRollback(err error) bool {
	retry, unsafe, complete := false, false, false
	failed := callback.Isolated("inspect transaction retry outcome", func() error {
		complete = errorgraph.Walk(err, func(current error) bool {
			if errorgraph.Matches(current, CommitUnknown) {
				unsafe = true
				return false
			}
			if found, present := errorgraph.AsShallow[*Error](current); present {
				if found == nil || found.Outcome() == Committed || found.Outcome() == Unknown || found.Outcome() == NoCommit {
					unsafe = true
					return false
				}
				if found.Outcome() == RolledBack {
					if found.Code() != SerializationFailure && found.Code() != Deadlock {
						unsafe = true
						return false
					}
					retry = true
				}
			}
			return true
		})
		return nil
	})
	return failed == nil && complete && retry && !unsafe
}
