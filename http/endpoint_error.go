package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// InvalidEndpointOption lets a typed adapter retain a construction failure in
// endpoint validation without replacing an earlier declaration error. The cause
// remains inspectable; its formatting methods are never invoked. A nil cause
// still produces an invalid declaration.
//
// Example: http.ConfigureEndpoint(endpoint, http.InvalidEndpointOption(cause))
func InvalidEndpointOption(cause error) EndpointOption {
	if cause == nil {
		cause = safeerror.NewFault(fault.Invalid, "endpoint requires a valid declaration")
	}
	return endpointOptionValue{kind: optionDeclarationError, declarationError: cause}
}
