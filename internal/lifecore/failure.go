package lifecore

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

// failure retains classification without formatting application errors or values.
type failure struct {
	message string
	cause   error
}

func (e *failure) Error() string    { return e.message }
func (e *failure) GoString() string { return e.Error() }
func (e *failure) Unwrap() error    { return e.cause }

// Failure wraps a callback failure with framework-owned safe text only.
// The message must never contain application values. Example: Failure("model before hook failed", err).
func Failure(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return safeerror.Retain(cause, safeerror.Owned(message, &failure{message: message, cause: cause}))
}
