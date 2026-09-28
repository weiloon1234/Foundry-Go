package invalid

import "foundry.test/consumer/unions"

var input unions.PaymentRequest
var delivery unions.Delivery

func invalid() { input.Method = delivery }
