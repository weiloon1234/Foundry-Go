package invalid

import (
	"foundry.test/consumer/security"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

func bad(delivery webhook.Delivery[security.BillingAccountID]) {
	var account security.CustomerAccountID = delivery.Account()
	_ = account
}
