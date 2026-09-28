package invalid

import (
	"context"
	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/httpclient"
)

func invalid(r httpclient.Response) {
	_, _ = httpclient.DecodeJSON[outgoing.CreateAccount](context.Background(), r, outgoing.AccountReceiptJSON())
}
