package redis

import (
	"context"
	"errors"
	"net"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// classify never formats server/transport errors, which can include credentials
// or payloads. The cause remains available for deliberate internal inspection.
func classify(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	var existing *fault.Error
	if errors.As(err, &existing) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, driver.ErrClosed) {
		return fault.Wrap(fault.Closed, "Redis connection is closed", err)
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return fault.Wrap(fault.Timeout, "Redis operation timed out", err)
	}
	return fault.Wrap(fault.Internal, "Redis operation failed", err)
}
