package clientcontracts

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// SearchPresentation demonstrates a typed, manually composed binding using the
// same metadata owner as generated client tags. It performs no request I/O.
func SearchPresentation() foundryhttp.QueryParameter[Member] {
	parameter := foundryhttp.QueryParam("display", foundryhttp.StringQuery[string](), func(member *Member) *string { return &member.Display })
	return parameter.WithPresentation(contract.Presentation{Kind: contract.TextPresentation, LabelKey: "fields.display"})
}
