package invalid

import (
	"foundry.test/consumer/clientcontracts"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ foundryhttp.QueryParameter[clientcontracts.Payload] = clientcontracts.SearchPresentation()
