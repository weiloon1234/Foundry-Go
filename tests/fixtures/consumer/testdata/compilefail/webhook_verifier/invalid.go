package invalid

import (
	"context"
	"foundry.test/consumer/security"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

func bad(verifier *webhook.Verifier[security.BillingAccountID], delivery webhook.Delivery[security.CustomerAccountID]) {
	_, _ = verifier.WithDelivery(context.Background(), delivery)
}
