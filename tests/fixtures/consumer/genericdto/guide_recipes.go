package genericdto

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func userEnvelopeResponse(userJSON contract.JSON[UserDTO]) foundryhttp.Response[Envelope[UserDTO]] {
	// doc-example:begin generic-dtos-2
	return foundryhttp.JSONResponse(200, EnvelopeJSON(userJSON))
	// doc-example:end generic-dtos-2
}

// doc-example:begin lookup-contract
// Lookup keeps JSON key and value descriptors separate.
//
//foundry:dto
type Lookup[K comparable, V any] struct {
	Values map[K]V `json:"values"`
}

// doc-example:end lookup-contract

func userLookupContract(userJSON contract.JSON[UserDTO]) contract.JSON[Lookup[string, UserDTO]] {
	// doc-example:begin generic-dtos-3
	descriptor := LookupJSON(
		contract.StringJSON[string](), contract.StringJSONKey[string](), userJSON,
	)
	// doc-example:end generic-dtos-3
	return descriptor
}
