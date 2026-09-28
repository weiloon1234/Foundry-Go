package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
)

func Encode() {
	_, _ = contract.Slice(httpdto.UserResponseJSON()).Encode(context.Background(), []httpdto.OrderResponse{}, contract.JSONLimits{})
}
