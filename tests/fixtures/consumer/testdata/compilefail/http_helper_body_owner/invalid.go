package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/http"
)

var _ http.Body[httpdto.OrderResponse] = httpdto.UpdateUserBody()
