package genericdto

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/http"
)

func userEnvelopeResponse(userJSON contract.JSON[UserDTO]) http.Response[Envelope[UserDTO]] {
	// doc-example:begin generic-dtos-2
	response := http.JSONResponse(200, EnvelopeJSON(userJSON))
	// doc-example:end generic-dtos-2
	return response
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
