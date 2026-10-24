// Package safeerror redacts extension failures without adding classifications.
package safeerror

// Wrap retains cause for inspection without invoking its formatting methods.
// A nil cause stays nil; message must be framework-owned diagnostic text.
func Wrap(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &redacted{message: message, cause: cause}
}

type redacted struct {
	message    string
	cause      error
	diagnostic bool
}

// Diagnostic retains a cause for errors.Is/As that must not supply an HTTP
// classification. Use only where retaining a previously discarded cause.
func Diagnostic(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &redacted{message: message, cause: cause, diagnostic: true}
}

// IsDiagnostic identifies the framework-owned boundary without calling error methods.
func IsDiagnostic(err error) bool {
	e, ok := err.(*redacted)
	return ok && e != nil && e.diagnostic
}

func (e *redacted) Error() string    { return e.message }
func (e *redacted) GoString() string { return e.message }
func (e *redacted) Unwrap() error    { return e.cause }
