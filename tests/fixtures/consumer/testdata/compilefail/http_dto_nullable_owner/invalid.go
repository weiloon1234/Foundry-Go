package invalid

import (
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = contract.JSON[value.Nullable[httpdto.OrderResponse]](contract.Nullable(httpdto.UserResponseJSON()))
