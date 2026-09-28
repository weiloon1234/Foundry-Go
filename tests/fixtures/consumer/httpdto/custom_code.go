package httpdto

import (
	"foundry.test/consumer/httpquery"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:dto
type TrackingResponse struct {
	Code    httpquery.TrackingCode                                 `json:"code"`
	History []httpquery.TrackingCode                               `json:"history,omitempty"`
	Next    value.Optional[value.Nullable[httpquery.TrackingCode]] `json:"next,omitzero"`
}

var TrackingContract = TrackingResponseJSON()

func TrackingContractDescription() {
	_ = TrackingContract.Validate()
}

// TrackingValueJSON exposes the same native value contract for a scalar endpoint.
func TrackingValueJSON() contract.JSON[httpquery.TrackingCode] {
	return httpquery.TrackingCode("").JSONContract()
}
