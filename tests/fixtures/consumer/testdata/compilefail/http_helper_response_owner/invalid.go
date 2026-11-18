package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/http"
)

var _ http.Response[httpdto.OrderResponse] = httpdto.UserResponseResponse(200)
