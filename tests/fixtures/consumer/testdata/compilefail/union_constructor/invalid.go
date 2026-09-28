package invalid

import "foundry.test/consumer/unions"

var _, _ = unions.PaymentMethodFromCard(unions.BankDTO{})
