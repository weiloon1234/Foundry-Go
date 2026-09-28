package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
)

func Decode() (httpdto.OrderResponse, error) {
	return httpdto.UserResponseJSON().Decode(context.Background(), nil, contract.JSONLimits{})
}
