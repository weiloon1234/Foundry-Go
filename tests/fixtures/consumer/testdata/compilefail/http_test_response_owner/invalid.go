package compilefail

import (
	"context"
	"foundry.test/consumer/outgoing"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/testkit/http"
)

func invalid(ctx context.Context, response httpclient.Response) {
	var receipt outgoing.CreateAccount
	receipt, _ = http.DecodeJSON(ctx, response, outgoing.AccountReceiptJSON())
	_ = receipt
}
