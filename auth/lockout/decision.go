package lockout

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type Status uint8

const (
	StatusAllowed Status = iota + 1
	StatusLocked
	StatusExpired
)

// Decision describes the authority's state after an operation. RetryAfter is
// relative to that operation. Triggered is true only for the failure that starts
// a lock; it can drive an in-process notification, never a durable delivery claim.
type Decision struct {
	Status     Status
	RetryAfter time.Duration
	Triggered  bool
}

func (d Decision) Validate(p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	switch d.Status {
	case StatusAllowed, StatusExpired:
		if d.RetryAfter == 0 && !d.Triggered {
			return nil
		}
	case StatusLocked:
		if d.RetryAfter > 0 && d.RetryAfter <= p.LockFor && d.RetryAfter%time.Millisecond == 0 {
			return nil
		}
	}
	return fault.New(fault.Internal, "invalid lockout decision")
}

// Admission returns a snapshot only for an allowed attempt.
type Admission struct {
	Decision Decision
	Snapshot Snapshot
}

func (a Admission) Validate(p Policy) error {
	if err := a.Decision.Validate(p); err != nil {
		return err
	}
	if a.Decision.Triggered {
		return fault.New(fault.Internal, "admission cannot start a lock")
	}
	if a.Decision.Status == StatusAllowed {
		return a.Snapshot.Validate()
	}
	if a.Decision.Status == StatusLocked && a.Snapshot == (Snapshot{}) {
		return nil
	}
	return fault.New(fault.Internal, "invalid lockout admission")
}

type Outcome uint8

const (
	Failed Outcome = iota + 1
	Succeeded
)

func (o Outcome) Validate() error {
	if o != Failed && o != Succeeded {
		return fault.New(fault.Invalid, "invalid lockout attempt outcome")
	}
	return nil
}
