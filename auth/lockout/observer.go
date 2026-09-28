package lockout

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Notice retains the declaration's concrete key for an explicit domain event or
// operational notification. Routine formatting/JSON hides account identifiers.
// This is an in-process observation after state commits, not durable delivery.
type Notice[K any] struct {
	key        K
	retryAfter time.Duration
}

func (n Notice[K]) Key() K                    { return n.key }
func (n Notice[K]) RetryAfter() time.Duration { return n.retryAfter }
func (Notice[K]) Format(s fmt.State, _ rune)  { _, _ = s.Write([]byte("login lockout notice")) }

// WithLockedObserver returns an immutable view. One confirmed threshold transition
// calls it once; already-locked denials do not. Lost backend replies can lose the
// notification. Errors cannot undo lockout and remain causes of its rejection.
func (t Throttle[K]) WithLockedObserver(observer func(context.Context, Notice[K]) error) (Throttle[K], error) {
	if err := t.Validate(); err != nil {
		return Throttle[K]{}, err
	}
	if observer == nil {
		return Throttle[K]{}, fault.New(fault.Invalid, "lockout observer is nil")
	}
	if t.observer != nil {
		return Throttle[K]{}, fault.New(fault.Duplicate, "lockout observer already configured")
	}
	t.observer = observer
	return t, nil
}
