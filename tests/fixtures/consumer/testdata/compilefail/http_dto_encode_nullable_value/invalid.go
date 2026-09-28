package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

func Encode() {
	_, _ = contract.Nullable(httpdto.UserResponseJSON()).Encode(context.Background(), value.Of(httpdto.OrderResponse{}), contract.JSONLimits{})
}
