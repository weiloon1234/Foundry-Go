package storage

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

// newFailure captures only a fresh framework diagnostic. Public Failure remains
// unmarked because callers may supply private operation or classification text.
func newFailure(code Code, operation Operation, outcome Outcome, cause error) error {
	return ownFailure(Failure(code, operation, outcome, cause))
}

// ownFailure is used after framework-owned outcome and cleanup fields settle.
// An error whose code was copied from an extension instead uses Retain.
func ownFailure(failure *Error) error {
	return safeerror.Owned(failure.Error(), failure)
}
