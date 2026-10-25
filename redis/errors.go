package redis

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"net"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// classify never formats server/transport errors, which can include credentials
// or payloads. The cause remains available for deliberate internal inspection.
// A command that completed successfully is a success even when its deadline
// expired after the reply arrived; only a failed command reports the context.
func classify(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	err = safeerror.Reveal("Redis operation failed", err)
	if ctx.Err() != nil {
		return safeerror.Wrap("Redis operation failed", safeerror.Join("Redis operation failed", ctx.Err(), err))
	}
	var existing *fault.Error
	if callback.ErrorAs(err, &existing) {
		return safeerror.Wrap("Redis operation failed", err)
	}
	if callback.ErrorIs(err, context.Canceled) || callback.ErrorIs(err, context.DeadlineExceeded) {
		return safeerror.Wrap("Redis operation failed", err)
	}
	if callback.ErrorIs(err, driver.ErrClosed) {
		return safeerror.WrapFault(fault.Closed, "Redis connection is closed", err)
	}
	var timeout net.Error
	timedOut := false
	inspected := callback.Isolated("Redis timeout classification", func() error {
		if callback.ErrorAs(err, &timeout) {
			timedOut = timeout.Timeout()
		}
		return nil
	})
	if inspected == nil && timedOut {
		return safeerror.WrapFault(fault.Timeout, "Redis operation timed out", err)
	}
	return safeerror.WrapFailure(fault.Internal, "Redis operation failed", err)
}
