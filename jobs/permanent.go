package jobs

// Permanent marks a failed attempt as terminal even when its retry policy has
// attempts remaining. It does not turn failure into success, acknowledge queue
// cancellation or undo execution. Wrapped/joined markers remain terminal.
// This is useful when repeating an external side effect may cause duplicates.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{cause: err}
}

type permanentError struct{ cause error }

func (*permanentError) Error() string   { return "job failed without automatic retry" }
func (e *permanentError) Unwrap() error { return e.cause }
