package invalid

import "foundry.test/consumer/unions"

var _, _ = unions.MatchPaymentMethod(unions.PaymentMethod{}, func(unions.CardDTO) (string, error) { return "", nil })
