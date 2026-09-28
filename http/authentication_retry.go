package http

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/lockout"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// authenticationRetryAfter snapshots only the framework's bounded lockout
// duration. Arbitrary error-chain methods still run in an owned callback.
func authenticationRetryAfter(err error) (time.Duration, error) {
	var retry time.Duration
	failure := callback.Isolated("HTTP lockout retry classification", func() error {
		rejected, found, complete := errorgraph.As[*lockout.Rejection](err)
		if !complete {
			return fault.New(fault.Invalid, "HTTP lockout retry classification exceeded traversal bounds")
		}
		if found {
			value := rejected.RetryAfter()
			if value > 0 && value <= lockout.MaxDuration {
				retry = value
			}
		}
		return nil
	})
	if failure != nil {
		return 0, failure
	}
	return retry, nil
}
