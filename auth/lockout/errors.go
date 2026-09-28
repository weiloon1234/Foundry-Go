package lockout

import "time"

type Code string

const (
	Locked      Code = "login_locked_out"
	Expired     Code = "login_attempt_expired"
	Unavailable Code = "login_protection_unavailable"
)

func (c Code) Error() string { return string(c) }

// Rejection carries a safe relative retry duration, never the submitted key.
// Runtime construction follows a validated backend decision.
type Rejection struct {
	retryAfter time.Duration
	cause      error
}

func (e *Rejection) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
func (e *Rejection) Error() string        { return Locked.Error() }
func (e *Rejection) Is(target error) bool { return target == Locked }
func (e *Rejection) RetryAfter() time.Duration {
	if e == nil {
		return 0
	}
	return e.retryAfter
}

type unavailable struct{ cause error }

func (e *unavailable) Error() string        { return Unavailable.Error() }
func (e *unavailable) Is(target error) bool { return target == Unavailable }
func (e *unavailable) Unwrap() error        { return e.cause }
func decisionError(d Decision) error {
	switch d.Status {
	case StatusLocked:
		return &Rejection{retryAfter: d.RetryAfter}
	case StatusExpired:
		return Expired
	}
	return nil
}
