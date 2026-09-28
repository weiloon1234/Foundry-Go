package httpquery

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// Priority retains its source identity in generated path/query and DTO metadata.
type Priority uint8

//foundry:path pattern=/priorities/{priority}
type PriorityPath struct{ Priority Priority }

//foundry:query
type PriorityQuery struct{ Priority Priority }

//foundry:dto
type PriorityResponse struct{ Priority Priority }

// PriorityCodec shows the generator's same-value source declaration boundary.
func PriorityCodec() foundryhttp.PathCodec[Priority] {
	return foundryhttp.URLType(contract.TypeID("foundry.test/consumer/httpquery.Priority"), foundryhttp.IntegerPath[Priority]())
}
