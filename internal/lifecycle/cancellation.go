package lifecycle

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

// CancellationOnly reports whether every leaf is ordinary cancellation. Joining a
// real failure with cancellation must retain that failure. The shared bounded
// walker keeps a cyclic or excessively deep graph from stranding shutdown.
func CancellationOnly(err error) bool {
	if err == nil {
		return false
	}
	cancelled, other := false, false
	inspected := callback.Isolated("classify lifecycle cancellation", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			if current == context.Canceled || current == context.DeadlineExceeded {
				cancelled = true
				return true
			}
			// A classified framework failure (for example an expired HTTP
			// shutdown grace) remains a failure even when its cause is a
			// context deadline. Plain wrapping/joining adds no classification.
			if _, classified := current.(*fault.Error); classified {
				other = true
				return false
			}
			switch wrapped := current.(type) {
			case interface{ Unwrap() []error }:
				if !slices.ContainsFunc(wrapped.Unwrap(), func(child error) bool { return child != nil }) {
					other = true
					return false
				}
			case interface{ Unwrap() error }:
				if wrapped.Unwrap() == nil {
					other = true
					return false
				}
			default:
				other = true
				return false
			}
			return true
		})
		other = other || !complete
		return nil
	})
	return inspected == nil && cancelled && !other
}
