package invalid

import (
	"foundry.test/consumer/httpdto"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

var _ foundryhttp.Body[httpdto.UpdateUser] = foundryhttp.JSONBody(httpdto.OrderResponseJSON())
