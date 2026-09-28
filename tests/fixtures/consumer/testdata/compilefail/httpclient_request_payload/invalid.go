package invalid

import (
	"context"
	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func invalid(r httpclient.Request) {
	_, _ = httpclient.JSON(context.Background(), r, outgoing.CreateAccountJSON(), outgoing.AccountReceipt{})
}
