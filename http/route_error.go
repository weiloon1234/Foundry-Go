package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// InvalidRouteRegistration lets a typed framework adapter retain a declaration
// failure at the existing heterogeneous router assembly boundary. NewRouter
// returns this cause before publishing any handler. It does not send an HTTP
// error response. A nil cause still produces an invalid registration.
func InvalidRouteRegistration(cause error) RouteRegistration {
	if cause == nil {
		cause = safeerror.NewFault(fault.Invalid, "route registration requires a valid declaration")
	}
	return RouteRegistration{err: cause}
}
