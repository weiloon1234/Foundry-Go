// Package auth provides model-first authentication and typed authorization.
// Credential strategies verify identity; providers load current models. Neither
// serialized attribution nor a model reference is evidence of authentication.
package auth

import "github.com/weiloon1234/Foundry-Go/fault"

// Code is a transport-independent, stable authentication failure.
type Code string

const (
	Unauthenticated Code = "unauthenticated"
	Forbidden       Code = "forbidden"
	MFARequired     Code = "mfa_required"
	// CredentialLimit rejects issuing another session/token when the subject's
	// active-credential cap is reached under a reject-new policy. It is a client
	// conflict (revoke an existing credential), not a server failure.
	CredentialLimit Code = "credential_limit"
	// ConfirmationRequired rejects a sensitive operation until the current
	// session recently re-entered its password.
	ConfirmationRequired Code = "password_confirmation_required"
	// ImpersonationForbidden rejects an operation that an impersonated session
	// must not perform, such as changing credentials or starting another
	// impersonation. It is a Forbidden-class refusal.
	ImpersonationForbidden Code = "impersonation_forbidden"
)

func (c Code) Error() string { return string(c) }

// WithCause retains diagnostic identity without formatting credential details.
func (c Code) WithCause(cause error) error { return &failure{code: c, cause: cause} }

type failure struct {
	code  Code
	cause error
}

func (e *failure) Error() string        { return e.code.Error() }
func (e *failure) Unwrap() error        { return e.cause }
func (e *failure) Is(target error) bool { return target == e.code }
func operationFailure(err error) error {
	if err == nil {
		return nil
	}
	return fault.Wrap(fault.Internal, "authentication operation failed", err)
}
