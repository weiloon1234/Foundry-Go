package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
)

var _ = contract.JSON[httpdto.OrderResponse](httpdto.UserResponseJSON())
