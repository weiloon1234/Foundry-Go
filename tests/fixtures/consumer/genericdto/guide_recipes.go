package genericdto

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin generic-dtos-2
func userEnvelopeResponse(userJSON contract.JSON[UserDTO]) http.Response[Envelope[UserDTO]] {
	response := http.JSONResponse(200, EnvelopeJSON(userJSON))
	return response
}

// doc-example:end generic-dtos-2

// doc-example:begin lookup-contract
// Lookup keeps JSON key and value descriptors separate.
//
//foundry:dto
type Lookup[K comparable, V any] struct {
	Values map[K]V `json:"values"`
}

// doc-example:end lookup-contract

// doc-example:begin generic-dtos-3
func userLookupContract(userJSON contract.JSON[UserDTO]) contract.JSON[Lookup[string, UserDTO]] {
	descriptor := LookupJSON(
		contract.StringJSON[string](), contract.StringJSONKey[string](), userJSON,
	)
	return descriptor
}

// doc-example:end generic-dtos-3
