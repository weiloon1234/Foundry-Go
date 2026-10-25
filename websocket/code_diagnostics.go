package websocket

import "github.com/weiloon1234/Foundry-Go/internal/safeerror"

// codeFailure captures protocol diagnostics at framework return sites. An
// application-defined Code remains untrusted, even though it has the same type.
func codeFailure(code Code) error {
	switch code {
	case Malformed, UnsupportedVersion, UnknownChannel, UnknownEvent,
		WrongDirection, Unauthenticated, Forbidden, NotSubscribed,
		AlreadySubscribed, InvalidPayload, CapacityExceeded, OperationFailed,
		OperationTimedOut, Stopping, RateLimited, Unavailable:
		return safeerror.Owned(string(code), code)
	default:
		return code
	}
}
