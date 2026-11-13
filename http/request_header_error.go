package http

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// RequestHeaderError reports invalid declared request headers without exposing
// submitted values or custom codec messages. Issue paths are JSON Pointers over
// canonical names; endpoints prefix them with /headers. Issues returns a copy.
type RequestHeaderError struct {
	issues []contract.Issue
	cause  error
}

// Error returns a safe message without formatting the retained cause.
func (*RequestHeaderError) Error() string { return "invalid request headers" }

// GoString returns the same redacted message.
func (e *RequestHeaderError) GoString() string { return e.Error() }

// Is identifies malformed header input or output as fault.Invalid.
func (*RequestHeaderError) Is(target error) bool { return target == fault.Invalid }

// Unwrap retains the original cause for deliberate internal inspection.
func (e *RequestHeaderError) Unwrap() error { return e.cause }

// Issues returns owned field diagnostics without raw header values.
func (e *RequestHeaderError) Issues() []contract.Issue { return slices.Clone(e.issues) }

func requestHeaderFailure(issues []contract.Issue, cause error) error {
	failure := &RequestHeaderError{issues: slices.Clone(issues), cause: cause}
	return safeerror.Owned(failure.Error(), failure)
}

func requestHeaderCallbackResult(ctx context.Context, callbackErr error, failed *queryBindingFailure, name string) error {
	if callbackErr != nil {
		return safeerror.WrapFailure(fault.Internal, "request header callback failed", callbackErr)
	}
	if failed != nil && failed.internal {
		return safeerror.WrapFailure(fault.Internal, "request header field binding failed", failed.cause)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed != nil {
		return requestHeaderFailure([]contract.Issue{{Path: queryIssuePath(name), Code: contract.ValueIssue}}, failed.cause)
	}
	return nil
}
